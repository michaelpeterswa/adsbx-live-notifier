package adsbx

import "testing"

func TestParseSBSNonMSG(t *testing.T) {
	if _, ok := parseSBS("ID,1,1,1,abc,1,2024/01/01,00:00:00.000,2024/01/01,00:00:00.000,FOO"); ok {
		t.Fatalf("expected non-MSG line to be rejected")
	}
}

func TestParseSBSShort(t *testing.T) {
	if _, ok := parseSBS("MSG,3,too,short"); ok {
		t.Fatalf("expected short MSG line to be rejected")
	}
}

func TestParseSBSFull(t *testing.T) {
	// 18 comma-separated fields per SBS BaseStation; we only consume a subset.
	line := "MSG,3,1,1,ABC123,1,2024/01/01,00:00:00.000,2024/01/01,00:00:00.000,UAL123  ,35000,450.5,180.2,47.6062,-122.3321,-1024,1200,0,0,0,0"
	ac, ok := parseSBS(line)
	if !ok {
		t.Fatalf("expected MSG line to parse")
	}
	if ac.Source != SourceSBS {
		t.Fatalf("source: got %q want %q", ac.Source, SourceSBS)
	}
	if ac.Hex != "abc123" {
		t.Fatalf("hex: got %q want %q", ac.Hex, "abc123")
	}
	if ac.Flight != "UAL123" {
		t.Fatalf("flight: got %q want %q", ac.Flight, "UAL123")
	}
	if ac.Squawk != "1200" {
		t.Fatalf("squawk: got %q want %q", ac.Squawk, "1200")
	}
	if ac.AltBaro == nil || *ac.AltBaro != 35000 {
		t.Fatalf("alt: got %v want 35000", ac.AltBaro)
	}
	if ac.GS == nil || *ac.GS != 450.5 {
		t.Fatalf("gs: got %v want 450.5", ac.GS)
	}
	if ac.Track == nil || *ac.Track != 180.2 {
		t.Fatalf("track: got %v want 180.2", ac.Track)
	}
	if ac.Lat == nil || *ac.Lat != 47.6062 {
		t.Fatalf("lat: got %v want 47.6062", ac.Lat)
	}
	if ac.Lon == nil || *ac.Lon != -122.3321 {
		t.Fatalf("lon: got %v want -122.3321", ac.Lon)
	}
	if ac.VS == nil || *ac.VS != -1024 {
		t.Fatalf("vs: got %v want -1024", ac.VS)
	}
}

func TestParseSBSPartial(t *testing.T) {
	// Position-only message (type 3 but missing GS/track/etc.)
	line := "MSG,3,1,1,DEF456,1,2024/01/01,00:00:00.000,2024/01/01,00:00:00.000,,,,,47.6,-122.3,,,,,,"
	ac, ok := parseSBS(line)
	if !ok {
		t.Fatalf("expected MSG line to parse")
	}
	if ac.Hex != "def456" {
		t.Fatalf("hex: got %q", ac.Hex)
	}
	if ac.Lat == nil || *ac.Lat != 47.6 {
		t.Fatalf("lat: got %v", ac.Lat)
	}
	if ac.GS != nil {
		t.Fatalf("expected nil GS, got %v", *ac.GS)
	}
	if ac.AltBaro != nil {
		t.Fatalf("expected nil AltBaro, got %v", *ac.AltBaro)
	}
}
