package adsbx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

type SBSStreamer struct {
	Addr string

	dialer net.Dialer
}

func NewSBSStreamer(addr string) *SBSStreamer {
	return &SBSStreamer{
		Addr:   addr,
		dialer: net.Dialer{Timeout: 5 * time.Second},
	}
}

func (s *SBSStreamer) Run(ctx context.Context, out chan<- Aircraft) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.streamOnce(ctx, out)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "sbs stream interrupted",
				slog.String("error", err.Error()),
				slog.Duration("retry_in", backoff),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = time.Second
	}
}

func (s *SBSStreamer) streamOnce(ctx context.Context, out chan<- Aircraft) error {
	conn, err := s.dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	slog.InfoContext(ctx, "sbs connected", slog.String("addr", s.Addr))

	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			ac, ok := parseSBS(strings.TrimRight(line, "\r\n"))
			if ok {
				select {
				case out <- ac:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("remote closed")
			}
			return err
		}
	}
}

// parseSBS parses a single CSV-ish SBS line. Only MSG rows carry data.
//
// Field indexes (0-based) per the SBS-1 BaseStation spec:
//
//	0  MSG
//	4  HexIdent
//	10 Callsign
//	11 Altitude
//	12 GroundSpeed
//	13 Track
//	14 Latitude
//	15 Longitude
//	16 VerticalRate
//	17 Squawk
func parseSBS(line string) (Aircraft, bool) {
	if !strings.HasPrefix(line, "MSG,") {
		return Aircraft{}, false
	}
	f := strings.Split(line, ",")
	if len(f) < 18 {
		return Aircraft{}, false
	}
	ac := Aircraft{
		Source:   SourceSBS,
		Received: time.Now().UTC(),
		Hex:      strings.ToLower(strings.TrimSpace(f[4])),
		Flight:   strings.TrimSpace(f[10]),
		Squawk:   strings.TrimSpace(f[17]),
	}
	if v, ok := parseFloat(f[14]); ok {
		ac.Lat = &v
	}
	if v, ok := parseFloat(f[15]); ok {
		ac.Lon = &v
	}
	if v, ok := parseInt32(f[11]); ok {
		ac.AltBaro = &v
	}
	if v, ok := parseFloat(f[12]); ok {
		ac.GS = &v
	}
	if v, ok := parseFloat(f[13]); ok {
		ac.Track = &v
	}
	if v, ok := parseInt32(f[16]); ok {
		ac.VS = &v
	}
	return ac, true
}

func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseInt32(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(v), true
}
