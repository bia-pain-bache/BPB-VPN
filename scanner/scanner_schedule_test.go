package scanner

import (
	"context"
	"sync"
	"testing"
	"time"
)

type scheduledPing struct {
	address string
	at      time.Time
}

type independentProbe struct {
	mu       sync.Mutex
	sessions map[string]*independentSession
	started  chan string
}

type independentSession struct {
	address string
	probe   *independentProbe
	release chan struct{}
	closed  chan struct{}
}

func (p *independentProbe) Open(_ context.Context, address string) (HTTPSession, error) {
	session := &independentSession{address: address, probe: p, release: make(chan struct{}), closed: make(chan struct{})}
	p.mu.Lock()
	p.sessions[address] = session
	p.mu.Unlock()
	return session, nil
}
func (*independentProbe) OpenBaseline(context.Context) (HTTPSession, error) {
	return immediateSession{}, nil
}
func (*independentProbe) Close() error { return nil }
func (s *independentSession) Ping(ctx context.Context) (time.Duration, error) {
	s.probe.started <- s.address
	select {
	case <-s.release:
		return 5 * time.Millisecond, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func (s *independentSession) Close() error {
	close(s.closed)
	return nil
}

func TestEachCandidateClosesAsSoonAsItsAttemptsFinish(t *testing.T) {
	probe := &independentProbe{sessions: make(map[string]*independentSession), started: make(chan string, 2)}
	scan := New(probe)
	if err := scan.Start(context.Background(), Options{
		Protocol: VLESS, SNI: "example.com", CIDRs: []string{"192.0.2.0/30"},
		ProbeCount: 2, OutputCount: 2, AttemptCount: 1, AttemptInterval: 10, ProbeInterval: 100,
	}); err != nil {
		t.Fatal(err)
	}
	first := <-probe.started
	second := <-probe.started
	probe.mu.Lock()
	firstSession, secondSession := probe.sessions[first], probe.sessions[second]
	probe.mu.Unlock()
	close(secondSession.release)
	select {
	case <-secondSession.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("completed candidate was not closed while another candidate was pending")
	}
	select {
	case <-firstSession.closed:
		t.Fatal("pending candidate was closed early")
	default:
	}
	close(firstSession.release)
	for event := range scan.Events() {
		if event.Type == "error" {
			t.Fatal(event.Message)
		}
	}
	select {
	case <-firstSession.closed:
	default:
		t.Fatal("first candidate was not closed")
	}
}

type scheduleProbe struct {
	started chan scheduledPing
	release chan struct{}
	mu      sync.Mutex
	active  map[string]int
	closed  map[string]bool
	early   bool
}

func newScheduleProbe() *scheduleProbe {
	return &scheduleProbe{
		started: make(chan scheduledPing, 100),
		release: make(chan struct{}),
		active:  make(map[string]int),
		closed:  make(map[string]bool),
	}
}

func (p *scheduleProbe) Open(_ context.Context, address string) (HTTPSession, error) {
	return &scheduleSession{probe: p, address: address}, nil
}

func (p *scheduleProbe) OpenBaseline(context.Context) (HTTPSession, error) {
	return &scheduleSession{probe: p, address: "baseline"}, nil
}

func (*scheduleProbe) Close() error { return nil }

type scheduleSession struct {
	probe   *scheduleProbe
	address string
}

func (s *scheduleSession) Ping(ctx context.Context) (time.Duration, error) {
	p := s.probe
	p.mu.Lock()
	p.active[s.address]++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.active[s.address]--
		p.mu.Unlock()
	}()
	p.started <- scheduledPing{address: s.address, at: time.Now()}
	select {
	case <-p.release:
		return 5 * time.Millisecond, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (s *scheduleSession) Close() error {
	p := s.probe
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active[s.address] != 0 {
		p.early = true
	}
	p.closed[s.address] = true
	return nil
}

func nextScheduled(t *testing.T, events <-chan scheduledPing) scheduledPing {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt launch")
		return scheduledPing{}
	}
}

func checkSpacing(t *testing.T, elapsed, want time.Duration) {
	t.Helper()
	if elapsed < want-20*time.Millisecond || elapsed > want+250*time.Millisecond {
		t.Fatalf("launch spacing = %v, want approximately %v", elapsed, want)
	}
}

func TestCandidateLaunchIntervalsOverlapAndStopCleansUp(t *testing.T) {
	probe := newScheduleProbe()
	scan := New(&baselineImmediateProbe{scheduleProbe: probe})
	err := scan.Start(context.Background(), Options{
		Protocol: VLESS, SNI: "example.com", CIDRs: []string{"192.0.2.0/29"},
		ProbeCount: 4, OutputCount: 4, AttemptCount: 3, AttemptInterval: 40, ProbeInterval: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		select {
		case <-probe.release:
		default:
			close(probe.release)
		}
	}()
	byIP := make(map[string][]time.Time)
	var order []string
	for len(order) < 3 || len(byIP[order[0]]) < 3 || len(byIP[order[1]]) < 3 {
		event := nextScheduled(t, probe.started)
		if len(byIP[event.address]) == 0 {
			order = append(order, event.address)
		}
		byIP[event.address] = append(byIP[event.address], event.at)
	}
	checkSpacing(t, byIP[order[0]][1].Sub(byIP[order[0]][0]), 40*time.Millisecond)
	checkSpacing(t, byIP[order[0]][2].Sub(byIP[order[0]][1]), 40*time.Millisecond)
	checkSpacing(t, byIP[order[1]][0].Sub(byIP[order[0]][0]), 150*time.Millisecond)
	checkSpacing(t, byIP[order[2]][0].Sub(byIP[order[1]][0]), 150*time.Millisecond)
	probe.mu.Lock()
	if probe.active[order[0]] != 3 {
		probe.mu.Unlock()
		t.Fatal("next IP did not start while the previous IP was pending")
	}
	probe.mu.Unlock()
	if err := scan.Stop(); err != nil {
		t.Fatal(err)
	}
	for event := range scan.Events() {
		if event.Type == "error" {
			t.Fatalf("scan failed: %s", event.Message)
		}
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.early || len(probe.closed) < 3 {
		t.Fatalf("cleanup: closed=%v early=%v", probe.closed, probe.early)
	}
}

func TestPauseStopsNewCandidateAttemptsUntilResumed(t *testing.T) {
	probe := newScheduleProbe()
	scan := New(probe)
	if err := scan.Start(context.Background(), Options{
		Protocol: VLESS, SNI: "example.com", CIDRs: []string{"192.0.2.0/30"},
		ProbeCount: 1, OutputCount: 1, AttemptCount: 3,
	}); err != nil {
		t.Fatal(err)
	}
	first := nextScheduled(t, probe.started)
	if err := scan.Pause(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-probe.started:
		t.Fatalf("attempt started while paused: %+v", event)
	case <-time.After(150 * time.Millisecond):
	}
	if err := scan.Resume(); err != nil {
		t.Fatal(err)
	}
	nextScheduled(t, probe.started)
	if err := scan.Stop(); err != nil {
		t.Fatal(err)
	}
	for range scan.Events() {
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if !probe.closed[first.address] || probe.early {
		t.Fatal("candidate was not cleaned up after cancellation")
	}
}

type baselineImmediateProbe struct{ *scheduleProbe }

func (*baselineImmediateProbe) OpenBaseline(context.Context) (HTTPSession, error) {
	return immediateSession{}, nil
}

type immediateSession struct{}

func (immediateSession) Ping(context.Context) (time.Duration, error) {
	return 5 * time.Millisecond, nil
}
func (immediateSession) Close() error { return nil }
