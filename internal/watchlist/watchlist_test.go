package watchlist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michaelpeterswa/adsbx-live-notifier/internal/adsbx"
)

func TestLoadWatchlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wl.json")
	body := `{"aircraft":[
		{"tail":"  n12345 ","description":"a"},
		{"hex":"AbC123","description":"b"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len: got %d want 2", len(entries))
	}
	if entries[0].Tail != "N12345" {
		t.Fatalf("tail not normalized: %q", entries[0].Tail)
	}
	if entries[1].Hex != "abc123" {
		t.Fatalf("hex not normalized: %q", entries[1].Hex)
	}
}

func TestLoadRejectsEmptyEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wl.json")
	body := `{"aircraft":[{"description":"no tail or hex"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for entry with neither tail nor hex")
	}
}

func TestLoadRejectsEmptyList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wl.json")
	if err := os.WriteFile(path, []byte(`{"aircraft":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for empty aircraft list")
	}
}

func TestMatchByTail(t *testing.T) {
	w, err := New([]Entry{{Tail: "N12345"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	entry, matched, fire := w.Match(adsbx.Aircraft{Flight: "n12345"})
	if !matched || !fire {
		t.Fatalf("expected match+fire on first sighting; matched=%v fire=%v", matched, fire)
	}
	if entry.Tail != "N12345" {
		t.Fatalf("entry: %+v", entry)
	}
}

func TestMatchByHex(t *testing.T) {
	w, err := New([]Entry{{Hex: "abc123"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, matched, fire := w.Match(adsbx.Aircraft{Hex: "ABC123"})
	if !matched || !fire {
		t.Fatalf("expected match+fire; matched=%v fire=%v", matched, fire)
	}
}

func TestMatchNoHit(t *testing.T) {
	w, err := New([]Entry{{Tail: "N12345"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, matched, _ := w.Match(adsbx.Aircraft{Flight: "OTHER", Hex: "deadbe"})
	if matched {
		t.Fatal("expected no match")
	}
}

func TestMatchCooldown(t *testing.T) {
	w, err := New([]Entry{{Tail: "N12345"}}, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, fire := w.Match(adsbx.Aircraft{Flight: "N12345"}); !fire {
		t.Fatal("first match should fire")
	}
	// ristretto admission is async; give it a moment so the second call sees the entry.
	time.Sleep(50 * time.Millisecond)
	if _, _, fire := w.Match(adsbx.Aircraft{Flight: "N12345"}); fire {
		t.Fatal("second match within cooldown should not fire")
	}
}
