package adsbx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type jsonAircraft struct {
	Hex      string   `json:"hex"`
	Flight   string   `json:"flight"`
	Squawk   string   `json:"squawk"`
	Lat      *float64 `json:"lat"`
	Lon      *float64 `json:"lon"`
	AltBaro  any      `json:"alt_baro"`
	GS       *float64 `json:"gs"`
	Track    *float64 `json:"track"`
	BaroRate *int32   `json:"baro_rate"`
	GeomRate *int32   `json:"geom_rate"`
	RSSI     *float64 `json:"rssi"`
	Seen     *float64 `json:"seen"`
}

type aircraftJSONPayload struct {
	Now      float64        `json:"now"`
	Messages int64          `json:"messages"`
	Aircraft []jsonAircraft `json:"aircraft"`
}

type JSONPoller struct {
	URL      string
	Interval time.Duration

	client *http.Client
}

func NewJSONPoller(url string, interval time.Duration) *JSONPoller {
	return &JSONPoller{
		URL:      url,
		Interval: interval,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *JSONPoller) Run(ctx context.Context, out chan<- Aircraft) error {
	t := time.NewTicker(p.Interval)
	defer t.Stop()

	if err := p.pollOnce(ctx, out); err != nil {
		slog.WarnContext(ctx, "json poll failed", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := p.pollOnce(ctx, out); err != nil {
				slog.WarnContext(ctx, "json poll failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (p *JSONPoller) pollOnce(ctx context.Context, out chan<- Aircraft) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var payload aircraftJSONPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	now := time.Unix(int64(payload.Now), 0).UTC()
	for _, ja := range payload.Aircraft {
		ac := Aircraft{
			Source:   SourceJSON,
			Received: now,
			Hex:      strings.ToLower(strings.TrimSpace(ja.Hex)),
			Flight:   strings.TrimSpace(ja.Flight),
			Squawk:   strings.TrimSpace(ja.Squawk),
			Lat:      ja.Lat,
			Lon:      ja.Lon,
			GS:       ja.GS,
			Track:    ja.Track,
			RSSI:     ja.RSSI,
			Seen:     ja.Seen,
		}
		if v, ok := ja.AltBaro.(float64); ok {
			i := int32(v)
			ac.AltBaro = &i
		}
		if ja.BaroRate != nil {
			ac.VS = ja.BaroRate
		} else if ja.GeomRate != nil {
			ac.VS = ja.GeomRate
		}
		select {
		case out <- ac:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
