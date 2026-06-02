package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLookupParsesAndCaches(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/api/v1/aircraft/abc123" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"ICAOTypeCode": "B738",
			"Manufacturer": "Boeing",
			"ModeS": "ABC123",
			"RegisteredOwners": "United Airlines",
			"Registration": "N12345",
			"Type": "Boeing 737-800"
		}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	md, err := c.Lookup(context.Background(), "ABC123")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if md.Registration != "N12345" || md.ICAOType != "B738" ||
		md.Type != "Boeing 737-800" || md.Operator != "United Airlines" {
		t.Fatalf("unexpected metadata: %+v", md)
	}
	if md.Empty() {
		t.Fatal("metadata should not be empty")
	}

	// ristretto admission is async; poll until the entry is cached, then assert
	// no further upstream hits occur.
	for range 100 {
		if _, ok := c.cache.Get("abc123"); ok {
			break
		}
		c.cache.Wait()
	}
	before := atomic.LoadInt32(&hits)
	if _, err := c.Lookup(context.Background(), "abc123"); err != nil {
		t.Fatalf("cached Lookup: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != before {
		t.Fatalf("expected cache hit, got %d upstream calls (was %d)", got, before)
	}
}

func TestLookupNotFoundIsCleanMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	md, err := c.Lookup(context.Background(), "deadbeef")
	if err != nil {
		t.Fatalf("404 should not error: %v", err)
	}
	if !md.Empty() {
		t.Fatalf("expected empty metadata, got %+v", md)
	}
}

func TestLookupEmptyHex(t *testing.T) {
	c, err := New("http://example.invalid")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	md, err := c.Lookup(context.Background(), "   ")
	if err != nil || !md.Empty() {
		t.Fatalf("empty hex should be a no-op, got md=%+v err=%v", md, err)
	}
}
