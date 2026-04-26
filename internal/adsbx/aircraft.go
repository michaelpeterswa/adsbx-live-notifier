package adsbx

import (
	"fmt"
	"strconv"
	"time"
)

type Source string

const (
	SourceJSON Source = "json"
	SourceSBS  Source = "sbs"
)

type Aircraft struct {
	Source   Source
	Received time.Time
	Hex      string
	Flight   string
	Squawk   string
	Lat      *float64
	Lon      *float64
	AltBaro  *int32
	GS       *float64
	Track    *float64
	VS       *int32
	RSSI     *float64
	Seen     *float64
}

func (a Aircraft) String() string {
	f := func(p *float64, prec int) string {
		if p == nil {
			return "-"
		}
		return strconv.FormatFloat(*p, 'f', prec, 64)
	}
	fi := func(p *int32) string {
		if p == nil {
			return "-"
		}
		return strconv.FormatInt(int64(*p), 10)
	}
	return fmt.Sprintf(
		"[%s] %s %-8s sq=%-4s lat=%s lon=%s alt=%s gs=%s trk=%s vs=%s",
		a.Source, a.Hex, a.Flight, a.Squawk,
		f(a.Lat, 4), f(a.Lon, 4), fi(a.AltBaro), f(a.GS, 0), f(a.Track, 0), fi(a.VS),
	)
}
