package scanner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fixedProbe struct {
	mu     sync.Mutex
	calls  int
	opens  int
	closes int
	last   string
	delay  time.Duration
}

type attemptProbe struct {
	mu     sync.Mutex
	delays []time.Duration
	failed int
	calls  int
	closed bool
}

func (p *attemptProbe) Open(context.Context, string) (HTTPSession, error) { return p, nil }
func (p *attemptProbe) OpenBaseline(context.Context) (HTTPSession, error) { return p, nil }
func (p *attemptProbe) Close() error                                      { p.closed = true; return nil }
func (p *attemptProbe) Ping(context.Context) (time.Duration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.calls
	p.calls++
	if i == p.failed {
		return 0, errors.New("attempt failed")
	}
	return p.delays[i], nil
}

func runCandidate(ctx context.Context, probe HTTPProbe, ip string, attempts int, threshold time.Duration) (Result, string, error) {
	session, err := probe.Open(ctx, ip)
	if err != nil {
		return Result{}, "http-failed", err
	}
	candidate := &candidateAttempts{ip: ip, session: session, results: make([]pingResult, attempts)}
	var pending sync.WaitGroup
	launched, _ := launchRounds(ctx, candidate, 0, attempts, time.Time{}, 10*time.Millisecond, workAllowed, &pending)
	pending.Wait()
	return candidate.finish(ctx, launched)
}

func workAllowed(ctx context.Context) bool { return ctx.Err() == nil }

func TestCandidateRunsEveryAttemptAndEvaluatesMean(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delays    []time.Duration
		failed    int
		rejection string
	}{
		{"slow first but fast mean", []time.Duration{9, 2, 1}, -1, ""},
		{"slow mean still accepted", []time.Duration{9, 9, 1}, -1, ""},
		{"failed attempt", []time.Duration{1, 1, 1}, 0, "http-failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &attemptProbe{delays: tc.delays, failed: tc.failed}
			_, rejection, err := runCandidate(context.Background(), probe, "192.0.2.1", 3, 5)
			if rejection != tc.rejection || probe.calls != 3 || !probe.closed {
				t.Fatalf("rejection=%s calls=%d closed=%v err=%v", rejection, probe.calls, probe.closed, err)
			}
			if (tc.failed >= 0) != (err != nil) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCandidateCancellationStopsAttemptsAndClosesSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := &attemptProbe{delays: []time.Duration{1, 1, 1}, failed: -1}
	_, rejection, err := runCandidate(ctx, probe, "192.0.2.1", 3, 5)
	if rejection != "cancelled" || err != nil || probe.calls != 0 || !probe.closed {
		t.Fatalf("rejection=%s calls=%d closed=%v err=%v", rejection, probe.calls, probe.closed, err)
	}
}

func TestFastestResultsAreRankedAndLimited(t *testing.T) {
	scan := New(&fixedProbe{})
	scan.addResult(Result{IP: "192.0.2.1", HTTPMillis: 100})
	scan.addResult(Result{IP: "192.0.2.2", HTTPMillis: 12})
	scan.addResult(Result{IP: "192.0.2.3", HTTPMillis: 30})
	results := topResults(scan.snapshotResults(), 2)
	if len(results) != 2 || results[0].IP != "192.0.2.2" || results[1].IP != "192.0.2.3" {
		t.Fatalf("unexpected fastest results: %+v", results)
	}
}

func (p *fixedProbe) Open(_ context.Context, address string) (HTTPSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opens++
	p.last = address
	return &fixedSession{probe: p}, nil
}
func (p *fixedProbe) OpenBaseline(ctx context.Context) (HTTPSession, error) {
	return p.Open(ctx, "baseline")
}

type fixedSession struct{ probe *fixedProbe }

func (s *fixedSession) Ping(context.Context) (time.Duration, error) {
	p := s.probe
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.delay, nil
}

func (s *fixedSession) Close() error { s.probe.closes++; return nil }
func (*fixedProbe) Close() error     { return nil }

func TestTunneledBaselineAndCandidateThreshold(t *testing.T) {
	host := "127.0.0.1"
	options := Options{Protocol: VLESS, SNI: host, AttemptCount: 3}
	probe := &fixedProbe{delay: 5 * time.Millisecond}
	threshold := 5 * time.Millisecond
	probe.calls = 0
	result, rejection, err := runCandidate(context.Background(), probe, host, options.AttemptCount, threshold)
	if err != nil || rejection != "" || probe.calls != options.AttemptCount || probe.opens != 1 || probe.closes != 1 || result.HTTPMillis != 5 {
		t.Fatalf("candidate: result=%+v rejection=%s calls=%d err=%v", result, rejection, probe.calls, err)
	}
	probe.calls = 0
	probe.delay = threshold + time.Millisecond
	_, rejection, err = runCandidate(context.Background(), probe, host, options.AttemptCount, threshold)
	if err != nil || rejection != "" || probe.calls != options.AttemptCount || probe.opens != 2 || probe.closes != 2 {
		t.Fatalf("slow candidate: rejection=%s calls=%d err=%v", rejection, probe.calls, err)
	}
}
