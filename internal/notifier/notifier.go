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
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/enrich"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/watchlist"
)

type Config struct {
	URL             string
	BearerToken     string
	PushoverUserKey string
	Priority        int
}

// Enricher resolves aircraft metadata from an ICAO hex. *enrich.Client
// satisfies it; it is an interface so the notifier can run without enrichment.
type Enricher interface {
	Lookup(ctx context.Context, hex string) (enrich.Metadata, error)
}

type Notifier struct {
	cfg      Config
	client   *http.Client
	queue    chan job
	enricher Enricher
}

type job struct {
	entry watchlist.Entry
	ac    adsbx.Aircraft
}

// New builds a Notifier. enricher may be nil to disable metadata lookups.
func New(cfg Config, enricher Enricher) *Notifier {
	return &Notifier{
		cfg:      cfg,
		client:   &http.Client{Timeout: 10 * time.Second},
		queue:    make(chan job, 64),
		enricher: enricher,
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
	data := map[string]string{
		"hex":    ac.Hex,
		"flight": strings.TrimSpace(ac.Flight),
		"source": string(ac.Source),
	}
	if ac.Lat != nil && ac.Lon != nil {
		data["position"] = fmt.Sprintf("%.5f,%.5f", *ac.Lat, *ac.Lon)
	}

	// Only the JSON "Aircraft Data" message is enriched. The SBS "Aircraft
	// Detected" alert stays minimal so it fires first without waiting on a
	// metadata lookup. Enrichment is best-effort: a failed lookup still alerts.
	var metaLine string
	if n.enricher != nil && ac.Source == adsbx.SourceJSON {
		md, err := n.enricher.Lookup(ctx, ac.Hex)
		if err != nil {
			slog.WarnContext(ctx, "metadata lookup failed",
				slog.String("hex", ac.Hex),
				slog.String("error", err.Error()),
			)
		}
		if !md.Empty() {
			if md.Registration != "" {
				data["registration"] = md.Registration
			}
			if md.Type != "" {
				data["type"] = md.Type
			}
			if md.ICAOType != "" {
				data["icao_type"] = md.ICAOType
			}
			if md.Manufacturer != "" {
				data["manufacturer"] = md.Manufacturer
			}
			if md.Operator != "" {
				data["operator"] = md.Operator
			}
			metaLine = metadataLine(md)
		}
	}

	title := truncate(titleForSource(ac.Source, e.Label()), 250)
	var b strings.Builder
	if e.Description != "" {
		b.WriteString(e.Description)
		b.WriteByte('\n')
	}
	if metaLine != "" {
		b.WriteString(metaLine)
		b.WriteByte('\n')
	}
	b.WriteString(ac.String())
	body := truncate(b.String(), 1024)

	payload := pulsarRequest{
		Content: pulsarContent{
			Title:    title,
			Body:     body,
			Priority: n.cfg.Priority,
			Data:     data,
		},
		Pushover: pulsarPushover{UserOrGroupKey: n.cfg.PushoverUserKey},
		// Bucket by 10-minute window so retries within a window dedupe
		// server-side. Keyed per source so the SBS detection and JSON data
		// messages are not deduped against each other at Pulsar.
		IdempotencyKey: fmt.Sprintf("adsb-%s-%s-%d", e.Label(), ac.Source, time.Now().Unix()/600),
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

// titleForSource gives each feed its own alert headline: SBS is an early bare
// detection, JSON follows with full flight data and metadata.
func titleForSource(src adsbx.Source, label string) string {
	switch src {
	case adsbx.SourceSBS:
		return fmt.Sprintf("Aircraft Detected: %s", label)
	case adsbx.SourceJSON:
		return fmt.Sprintf("Aircraft Detected (Data): %s", label)
	default:
		return fmt.Sprintf("Aircraft spotted: %s", label)
	}
}

// metadataLine renders resolved metadata as a single human-readable line,
// e.g. "N12345 — Boeing 737-800 (B738) — United Airlines".
func metadataLine(md enrich.Metadata) string {
	typ := md.Type
	if typ == "" {
		typ = md.Manufacturer
	}
	if typ != "" && md.ICAOType != "" {
		typ = fmt.Sprintf("%s (%s)", typ, md.ICAOType)
	} else if typ == "" {
		typ = md.ICAOType
	}

	parts := make([]string, 0, 3)
	if md.Registration != "" {
		parts = append(parts, md.Registration)
	}
	if typ != "" {
		parts = append(parts, typ)
	}
	if md.Operator != "" {
		parts = append(parts, md.Operator)
	}
	return strings.Join(parts, " — ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
