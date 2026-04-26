package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/michaelpeterswa/adsbx-live-notifier/internal/adsbx"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/watchlist"
)

type Config struct {
	URL             string
	BearerToken     string
	PushoverUserKey string
	Priority        int
}

type Notifier struct {
	cfg    Config
	client *http.Client
	queue  chan job
}

type job struct {
	entry watchlist.Entry
	ac    adsbx.Aircraft
}

func New(cfg Config) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		queue:  make(chan job, 64),
	}
}

func (n *Notifier) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case j := <-n.queue:
			if err := n.send(ctx, j.entry, j.ac); err != nil {
				slog.ErrorContext(ctx, "notify failed",
					slog.String("label", j.entry.Label()),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

func (n *Notifier) Notify(ctx context.Context, entry watchlist.Entry, ac adsbx.Aircraft) {
	select {
	case n.queue <- job{entry: entry, ac: ac}:
	default:
		slog.WarnContext(ctx, "notify queue full, dropping alert",
			slog.String("label", entry.Label()),
		)
	}
}

type pulsarRequest struct {
	Content        pulsarContent  `json:"content"`
	Pushover       pulsarPushover `json:"pushover"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
}

type pulsarContent struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Priority int               `json:"priority,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
}

type pulsarPushover struct {
	UserOrGroupKey string `json:"userOrGroupKey"`
}

func (n *Notifier) send(ctx context.Context, e watchlist.Entry, ac adsbx.Aircraft) error {
	title := truncate(fmt.Sprintf("Aircraft spotted: %s", e.Label()), 250)
	body := ac.String()
	if e.Description != "" {
		body = e.Description + "\n" + body
	}
	body = truncate(body, 1024)

	data := map[string]string{
		"hex":    ac.Hex,
		"flight": strings.TrimSpace(ac.Flight),
		"source": string(ac.Source),
	}
	if ac.Lat != nil && ac.Lon != nil {
		data["position"] = fmt.Sprintf("%.5f,%.5f", *ac.Lat, *ac.Lon)
	}

	payload := pulsarRequest{
		Content: pulsarContent{
			Title:    title,
			Body:     body,
			Priority: n.cfg.Priority,
			Data:     data,
		},
		Pushover: pulsarPushover{UserOrGroupKey: n.cfg.PushoverUserKey},
		// Bucket by 10-minute window so retries within a window dedupe server-side.
		IdempotencyKey: fmt.Sprintf("adsb-%s-%d", e.Label(), time.Now().Unix()/600),
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	url := strings.TrimRight(n.cfg.URL, "/") + "/notifications"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.cfg.BearerToken)

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	slog.InfoContext(ctx, "alerted",
		slog.String("label", e.Label()),
		slog.String("hex", ac.Hex),
		slog.String("flight", strings.TrimSpace(ac.Flight)),
		slog.String("source", string(ac.Source)),
	)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
