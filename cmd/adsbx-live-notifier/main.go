package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"alpineworks.io/ootel"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/michaelpeterswa/adsbx-live-notifier/internal/adsbx"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/config"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/logging"
	appmetrics "github.com/michaelpeterswa/adsbx-live-notifier/internal/metrics"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/notifier"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/watchlist"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}

	slogLevel, err := logging.LogLevelToSlogLevel(logLevel)
	if err != nil {
		log.Fatalf("could not convert log level: %s", err)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	})))

	c, err := config.NewConfig()
	if err != nil {
		slog.Error("could not create config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if c.TracingVersion == "" {
		c.TracingVersion = version
	}

	slog.Info("starting adsbx-live-notifier", slog.String("version", version))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	exporterType := ootel.ExporterTypePrometheus
	if c.Local {
		exporterType = ootel.ExporterTypeOTLPGRPC
	}

	ootelClient := ootel.NewOotelClient(
		ootel.WithMetricConfig(
			ootel.NewMetricConfig(
				c.MetricsEnabled,
				exporterType,
				c.MetricsPort,
			),
		),
		ootel.WithTraceConfig(
			ootel.NewTraceConfig(
				c.TracingEnabled,
				c.TracingSampleRate,
				c.TracingService,
				c.TracingVersion,
			),
		),
	)

	shutdown, err := ootelClient.Init(ctx)
	if err != nil {
		slog.Error("could not create ootel client", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = shutdown(shutdownCtx)
	}()

	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(5 * time.Second)); err != nil {
		slog.Error("could not create runtime metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := host.Start(); err != nil {
		slog.Error("could not create host metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	m, err := appmetrics.New()
	if err != nil {
		slog.Error("could not create metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var (
		wl   *watchlist.Watchlist
		ntfr *notifier.Notifier
	)
	if c.WatchlistPath != "" {
		entries, err := watchlist.Load(c.WatchlistPath)
		if err != nil {
			slog.Error("could not load watchlist",
				slog.String("path", c.WatchlistPath),
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}
		wl, err = watchlist.New(entries, c.WatchlistCooldown)
		if err != nil {
			slog.Error("could not create watchlist", slog.String("error", err.Error()))
			os.Exit(1)
		}
		ntfr = notifier.New(notifier.Config{
			URL:             c.PulsarURL,
			BearerToken:     c.PulsarBearerToken,
			PushoverUserKey: c.PulsarPushoverUserKey,
			Priority:        c.PulsarPriority,
		})
		slog.Info("watchlist loaded",
			slog.Int("entries", wl.Len()),
			slog.Duration("cooldown", c.WatchlistCooldown),
		)
	} else {
		slog.Info("watchlist not configured; notifier disabled")
	}

	out := make(chan adsbx.Aircraft, 1024)
	var wg sync.WaitGroup

	if ntfr != nil {
		wg.Go(func() {
			if err := ntfr.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("notifier exited", slog.String("error", err.Error()))
			}
		})
	}

	if c.ADSBXJSONEnabled {
		poller := adsbx.NewJSONPoller(c.ADSBXJSONURL, c.ADSBXPollInterval)
		wg.Go(func() {
			slog.Info("starting json poller",
				slog.String("url", c.ADSBXJSONURL),
				slog.Duration("interval", c.ADSBXPollInterval),
			)
			if err := poller.Run(ctx, out); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("json poller exited", slog.String("error", err.Error()))
			}
		})
	}

	if c.ADSBXSBSEnabled {
		streamer := adsbx.NewSBSStreamer(c.ADSBXSBSAddr)
		wg.Go(func() {
			slog.Info("starting sbs streamer", slog.String("addr", c.ADSBXSBSAddr))
			if err := streamer.Run(ctx, out); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("sbs streamer exited", slog.String("error", err.Error()))
			}
		})
	}

	wg.Go(func() {
		consume(ctx, out, wl, ntfr, m)
	})

	<-ctx.Done()
	slog.Info("shutdown signal received")
	wg.Wait()
	slog.Info("shutdown complete")
}

func consume(
	ctx context.Context,
	in <-chan adsbx.Aircraft,
	wl *watchlist.Watchlist,
	ntfr *notifier.Notifier,
	m *appmetrics.Metrics,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case ac := <-in:
			m.MessagesReceived.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source", string(ac.Source)),
			))
			slog.Debug("aircraft observed",
				slog.String("source", string(ac.Source)),
				slog.String("hex", ac.Hex),
				slog.String("flight", ac.Flight),
				slog.String("squawk", ac.Squawk),
			)
			if wl == nil {
				continue
			}
			entry, matched, fire := wl.Match(ac)
			if !matched {
				continue
			}
			labelAttr := attribute.String("label", entry.Label())
			m.WatchlistMatches.Add(ctx, 1, metric.WithAttributes(labelAttr))
			if !fire {
				continue
			}
			m.AlertsFired.Add(ctx, 1, metric.WithAttributes(labelAttr))
			if ntfr != nil {
				ntfr.Notify(ctx, entry, ac)
			}
		}
	}
}
