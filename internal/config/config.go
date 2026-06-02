package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	LogLevel string `env:"LOG_LEVEL" envDefault:"error"`

	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"METRICS_PORT" envDefault:"8081"`

	Local bool `env:"LOCAL" envDefault:"false"`

	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"false"`
	TracingSampleRate float64 `env:"TRACING_SAMPLERATE" envDefault:"0.01"`
	TracingService    string  `env:"TRACING_SERVICE" envDefault:"adsbx-live-notifier"`
	TracingVersion    string  `env:"TRACING_VERSION"`

	ADSBXHost         string        `env:"ADSBX_HOST" envDefault:"adsbexchange.local"`
	ADSBXJSONURL      string        `env:"ADSBX_JSON_URL"`
	ADSBXSBSAddr      string        `env:"ADSBX_SBS_ADDR"`
	ADSBXPollInterval time.Duration `env:"ADSBX_POLL_INTERVAL" envDefault:"1s"`
	ADSBXJSONEnabled  bool          `env:"ADSBX_JSON_ENABLED" envDefault:"true"`
	ADSBXSBSEnabled   bool          `env:"ADSBX_SBS_ENABLED" envDefault:"true"`

	WatchlistPath     string        `env:"WATCHLIST_PATH"`
	WatchlistCooldown time.Duration `env:"WATCHLIST_COOLDOWN" envDefault:"10m"`

	MetadataEnabled bool   `env:"METADATA_ENABLED" envDefault:"true"`
	HexDBURL        string `env:"HEXDB_URL" envDefault:"https://hexdb.io"`

	PulsarURL             string `env:"PULSAR_URL"`
	PulsarBearerToken     string `env:"PULSAR_BEARER_TOKEN"`
	PulsarPushoverUserKey string `env:"PULSAR_PUSHOVER_USER_KEY"`
	PulsarPriority        int    `env:"PULSAR_PRIORITY" envDefault:"0"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if cfg.ADSBXJSONURL == "" {
		cfg.ADSBXJSONURL = fmt.Sprintf("http://%s/tar1090/data/aircraft.json", cfg.ADSBXHost)
	}
	if cfg.ADSBXSBSAddr == "" {
		cfg.ADSBXSBSAddr = fmt.Sprintf("%s:30003", cfg.ADSBXHost)
	}

	if !cfg.ADSBXJSONEnabled && !cfg.ADSBXSBSEnabled {
		return nil, fmt.Errorf("at least one of ADSBX_JSON_ENABLED or ADSBX_SBS_ENABLED must be true")
	}

	if cfg.WatchlistPath != "" {
		if cfg.PulsarURL == "" {
			return nil, fmt.Errorf("PULSAR_URL is required when WATCHLIST_PATH is set")
		}
		if cfg.PulsarBearerToken == "" {
			return nil, fmt.Errorf("PULSAR_BEARER_TOKEN is required when WATCHLIST_PATH is set")
		}
		if cfg.PulsarPushoverUserKey == "" {
			return nil, fmt.Errorf("PULSAR_PUSHOVER_USER_KEY is required when WATCHLIST_PATH is set")
		}
	}

	return &cfg, nil
}
