package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingProbe struct {
	mu       sync.Mutex
	opened   map[string]int
	pings    map[string]int
	closed   map[string]int
	closeErr error
}

func (p *countingProbe) Open(_ context.Context, address string) (HTTPSession, error) {
	p.mu.Lock()
	p.opened[address]++
	p.mu.Unlock()
	return &countingSession{probe: p, address: address}, nil
}
func (p *countingProbe) OpenBaseline(ctx context.Context) (HTTPSession, error) {
	return p.Open(ctx, "baseline")
}

func (*countingProbe) Close() error { return nil }

type countingSession struct {
	probe   *countingProbe
	address string
}

func (s *countingSession) Ping(context.Context) (time.Duration, error) {
	s.probe.mu.Lock()
	s.probe.pings[s.address]++
	s.probe.mu.Unlock()
	return 5 * time.Millisecond, nil
}

func (s *countingSession) Close() error {
	s.probe.mu.Lock()
	s.probe.closed[s.address]++
	s.probe.mu.Unlock()
	return s.probe.closeErr
}

func TestScannerRunsAllCandidatesThroughHTTP(t *testing.T) {
	probe := &countingProbe{opened: map[string]int{}, pings: map[string]int{}, closed: map[string]int{}}
	scan := New(probe)
	err := scan.Start(context.Background(), Options{
		Protocol: VLESS, SNI: "127.0.0.1", CIDRs: []string{"192.0.2.0/24"},
		ProbeCount: 100, OutputCount: 100, AttemptCount: 3, AttemptInterval: 10, ProbeInterval: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	var finished bool
	for event := range scan.Events() {
		if event.Type == "error" {
			t.Fatalf("scan failed: %s", event.Message)
		}
		if event.Type == "done" {
			finished = true
			if len(event.Results) != 100 {
				t.Fatalf("unexpected results: %d", len(event.Results))
			}
		}
	}
	if !finished {
		t.Fatal("scanner did not emit done")
	}
	if len(probe.opened) != 100 {
		t.Fatalf("candidate outbound count: %d", len(probe.opened))
	}
	for ip, opened := range probe.opened {
		expectedPings := 3
		if opened != 1 || probe.closed[ip] != 1 || probe.pings[ip] != expectedPings {
			t.Errorf("%s: opened=%d closed=%d pings=%d", ip, opened, probe.closed[ip], probe.pings[ip])
		}
	}
}

func TestSessionCloseFailuresAreReported(t *testing.T) {
	probe := &countingProbe{
		opened: map[string]int{}, pings: map[string]int{}, closed: map[string]int{},
		closeErr: errors.New("close failed"),
	}

	options := Options{SNI: "127.0.0.1", AttemptCount: 1}
	result, rejection, err := runCandidate(context.Background(), probe, "192.0.2.1", options.AttemptCount, time.Second)
	if rejection != "http-failed" || err == nil || !strings.Contains(err.Error(), "close candidate outbound") || result.IP != "" {
		t.Fatalf("unexpected candidate result: %+v %s %v", result, rejection, err)
	}

}

func TestResultEventContainsOnlyIPAndDelay(t *testing.T) {
	payload, err := json.Marshal(Event{
		Type:    "done",
		Results: []Result{{IP: "192.0.2.1", HTTPMillis: 12.5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"type":"done","results":[{"ip":"192.0.2.1","httpMillis":12.5}]}` {
		t.Fatalf("unexpected event payload: %s", payload)
	}
}
