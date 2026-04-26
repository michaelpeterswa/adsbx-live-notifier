package metrics

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/michaelpeterswa/adsbx-live-notifier"

type Metrics struct {
	MessagesReceived metric.Int64Counter
	WatchlistMatches metric.Int64Counter
	AlertsFired      metric.Int64Counter
}

func New() (*Metrics, error) {
	meter := otel.Meter(meterName)

	messages, err := meter.Int64Counter(
		"adsbx_messages_received_total",
		metric.WithDescription("Total ADS-B messages received from feeders, by source."),
	)
	if err != nil {
		return nil, fmt.Errorf("messages counter: %w", err)
	}
	matches, err := meter.Int64Counter(
		"adsbx_watchlist_matches_total",
		metric.WithDescription("Total watchlist matches (pre-cooldown)."),
	)
	if err != nil {
		return nil, fmt.Errorf("matches counter: %w", err)
	}
	alerts, err := meter.Int64Counter(
		"adsbx_alerts_fired_total",
		metric.WithDescription("Total alerts dispatched to the notifier (post-cooldown)."),
	)
	if err != nil {
		return nil, fmt.Errorf("alerts counter: %w", err)
	}
	return &Metrics{
		MessagesReceived: messages,
		WatchlistMatches: matches,
		AlertsFired:      alerts,
	}, nil
}
