package watchlist

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/michaelpeterswa/adsbx-live-notifier/internal/adsbx"
)

// Entry is one configured aircraft to watch. Only Tail/Hex/Description are
// configurable in the watchlist file — everything else (cooldown, notifier
// credentials, feeder URLs) is supplied through environment variables.
type Entry struct {
	Tail        string `json:"tail"`
	Hex         string `json:"hex"`
	Description string `json:"description"`
}

func (e Entry) Label() string {
	if e.Tail != "" {
		return e.Tail
	}
	return e.Hex
}

// File is the on-disk representation of the watchlist file. It deliberately
// contains only `aircraft`; cooldowns and notifier config live in env vars.
type File struct {
	Aircraft []Entry `json:"aircraft"`
}

func Load(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read watchlist: %w", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse watchlist: %w", err)
	}
	if len(f.Aircraft) == 0 {
		return nil, fmt.Errorf("watchlist contains no aircraft")
	}
	out := make([]Entry, 0, len(f.Aircraft))
	for i, e := range f.Aircraft {
		e.Tail = strings.ToUpper(strings.TrimSpace(e.Tail))
		e.Hex = strings.ToLower(strings.TrimSpace(e.Hex))
		if e.Tail == "" && e.Hex == "" {
			return nil, fmt.Errorf("watchlist[%d]: tail or hex required", i)
		}
		out = append(out, e)
	}
	return out, nil
}

type Watchlist struct {
	entries  []Entry
	cooldown time.Duration
	cache    *ristretto.Cache[string, struct{}]
}

func New(entries []Entry, cooldown time.Duration) (*Watchlist, error) {
	cache, err := ristretto.NewCache(&ristretto.Config[string, struct{}]{
		NumCounters: 1 << 14,
		MaxCost:     1 << 14,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("ristretto: %w", err)
	}
	return &Watchlist{
		entries:  entries,
		cooldown: cooldown,
		cache:    cache,
	}, nil
}

func (w *Watchlist) Len() int { return len(w.entries) }

// Match returns the first matching entry. fire is true when the cooldown has
// lapsed for that entry and the caller should send an alert.
func (w *Watchlist) Match(ac adsbx.Aircraft) (entry Entry, matched, fire bool) {
	flight := strings.ToUpper(strings.TrimSpace(ac.Flight))
	hex := strings.ToLower(strings.TrimSpace(ac.Hex))
	for _, e := range w.entries {
		hit := (e.Hex != "" && e.Hex == hex) || (e.Tail != "" && flight != "" && e.Tail == flight)
		if !hit {
			continue
		}
		key := e.Label()
		if _, present := w.cache.Get(key); present {
			return e, true, false
		}
		// SetWithTTL is async — a burst of position updates within the same
		// tick may produce up to one duplicate before admission, which is fine.
		w.cache.SetWithTTL(key, struct{}{}, 1, w.cooldown)
		return e, true, true
	}
	return Entry{}, false, false
}
