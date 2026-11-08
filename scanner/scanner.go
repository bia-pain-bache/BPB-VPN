package scanner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

type Result struct {
	IP         string  `json:"ip"`
	HTTPMillis float64 `json:"httpMillis"`
}

type Event struct {
	Type      string   `json:"type"`
	Phase     string   `json:"phase,omitempty"`
	Completed int      `json:"completed,omitempty"`
	Total     int      `json:"total,omitempty"`
	Results   []Result `json:"results,omitempty"`
	Message   string   `json:"message,omitempty"`
}

type Scanner struct {
	mu       sync.Mutex
	cond     *sync.Cond
	paused   bool
	stopping bool
	running  bool
	cancel   context.CancelFunc
	events   chan Event
	results  []Result
	probe    HTTPProbe
}

type HTTPProbe interface {
	Open(context.Context, string) (HTTPSession, error)
	Close() error
}

type HTTPSession interface {
	// Ping can be called concurrently; Close is called after all pings return.
	Ping(context.Context) (time.Duration, error)
	Close() error
}

func New(probe HTTPProbe) *Scanner {
	s := &Scanner{events: make(chan Event, 128), probe: probe}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *Scanner) Events() <-chan Event { return s.events }

func (s *Scanner) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scanner) Start(parent context.Context, options Options) error {
	if s.probe == nil {
		return errors.New("Xray HTTP probe is required")
	}
	validated, err := options.withDefaultsAndValidation()
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("scanner is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	s.cancel = cancel
	s.paused = false
	s.stopping = false
	s.running = true
	s.results = nil
	s.events = make(chan Event, 128)
	events := s.events
	s.mu.Unlock()
	go s.run(ctx, validated, events)
	return nil
}

func (s *Scanner) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return errors.New("scanner is not running")
	}
	s.paused = true
	return nil
}

func (s *Scanner) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return errors.New("scanner is not running")
	}
	s.paused = false
	s.cond.Broadcast()
	return nil
}

func (s *Scanner) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return errors.New("scanner is not running")
	}
	s.stopping = true
	s.paused = false
	s.cond.Broadcast()
	s.cancel()
	return nil
}

func (s *Scanner) waitForWork(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.paused && !s.stopping && ctx.Err() == nil {
		s.cond.Wait()
	}
	return !s.stopping && ctx.Err() == nil
}

func (s *Scanner) addResult(result Result) {
	s.mu.Lock()
	s.results = append(s.results, result)
	s.mu.Unlock()
}

func (s *Scanner) snapshotResults() []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := append([]Result(nil), s.results...)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].HTTPMillis < results[j].HTTPMillis
	})
	if len(results) > 0 {
		results = results[:min(len(results), 10_000)]
	}
	return results
}

func (s *Scanner) emit(events chan Event, event Event) {
	select {
	case events <- event:
	default:
		if event.Type == "progress" {
			return
		}
		select {
		case <-events:
		default:
		}
		select {
		case events <- event:
		default:
		}
	}
}

func (s *Scanner) finish(events chan Event, event Event) {
	if err := s.probe.Close(); err != nil {
		event.Type = "error"
		event.Message = fmt.Sprintf("close scanner Xray instance: %v", err)
	}
	s.mu.Lock()
	s.running = false
	s.paused = false
	s.stopping = false
	s.cancel = nil
	s.mu.Unlock()
	s.emit(events, event)
	close(events)
}

func (s *Scanner) run(ctx context.Context, options Options, events chan Event) {
	attemptInterval := time.Duration(options.AttemptInterval) * time.Millisecond
	probeInterval := time.Duration(options.ProbeInterval) * time.Millisecond
	candidates, err := SelectScanIPs(ctx, options.CIDRs, options.ProbeCount, options.EnableIPv6)
	if err != nil {
		if ctx.Err() != nil {
			s.finish(events, Event{Type: "stopped"})
			return
		}
		s.finish(events, Event{Type: "error", Message: err.Error()})
		return
	}
	var candidatesInFlight sync.WaitGroup
	var completedMu sync.Mutex
	completed := 0
	report := func(ip string, result Result, rejection string, probeErr error) {
		if rejection == "" {
			s.addResult(result)
		}
		if probeErr != nil {
			log.Printf("scanner candidate %s: %v", ip, probeErr)
		}
		completedMu.Lock()
		completed++
		done := completed
		completedMu.Unlock()
		s.emit(events, Event{Type: "progress", Phase: "scan", Completed: done, Total: len(candidates)})
	}
	var nextIP time.Time
	for _, ip := range candidates {
		if !waitUntil(ctx, nextIP) || !s.waitForWork(ctx) {
			break
		}
		session, err := s.probe.Open(ctx, ip)
		if err != nil {
			if ctx.Err() == nil {
				report(ip, Result{}, "http-failed", fmt.Errorf("create candidate outbound: %w", err))
			}
			nextIP = time.Now().Add(probeInterval)
			continue
		}
		candidate := &candidateAttempts{
			ip: ip, session: session, results: make([]pingResult, options.AttemptCount),
		}
		if !s.waitForWork(ctx) {
			if err := session.Close(); err != nil {
				log.Printf("scanner candidate %s: close candidate outbound: %v", ip, err)
			}
			break
		}
		firstLaunch := time.Now()
		var attempts sync.WaitGroup
		launchAttempt(ctx, candidate, 0, &attempts)
		nextIP = firstLaunch.Add(probeInterval)
		candidatesInFlight.Add(1)
		go func() {
			defer candidatesInFlight.Done()
			launched, _ := launchRounds(ctx, candidate, 1, options.AttemptCount, firstLaunch.Add(attemptInterval), attemptInterval, s.waitForWork, &attempts)
			attempts.Wait()
			result, rejection, err := candidate.finish(ctx, launched)
			if rejection != "cancelled" {
				report(candidate.ip, result, rejection, err)
			} else if err != nil {
				log.Printf("scanner candidate %s: %v", candidate.ip, err)
			}
		}()
	}
	candidatesInFlight.Wait()
	results := s.snapshotResults()
	if ctx.Err() != nil {
		s.finish(events, Event{Type: "stopped", Results: topResults(results, options.OutputCount)})
		return
	}
	s.finish(events, Event{Type: "done", Results: topResults(results, options.OutputCount)})
}

type pingResult struct {
	delay time.Duration
	err   error
}

type candidateAttempts struct {
	ip      string
	session HTTPSession
	results []pingResult
}

func waitUntil(ctx context.Context, at time.Time) bool {
	if delay := time.Until(at); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return false
		}
	}
	return ctx.Err() == nil
}

func launchAttempt(ctx context.Context, candidate *candidateAttempts, attempt int, pending *sync.WaitGroup) {
	pending.Add(1)
	go func() {
		defer pending.Done()
		delay, err := candidate.session.Ping(ctx)
		candidate.results[attempt] = pingResult{delay, err}
	}()
}

func launchRounds(ctx context.Context, candidate *candidateAttempts, start, count int, first time.Time, interval time.Duration, waitForWork func(context.Context) bool, pending *sync.WaitGroup) (int, time.Time) {
	var last time.Time
	for attempt := start; attempt < count; attempt++ {
		target := first
		if attempt > start {
			target = last.Add(interval)
		}
		if !waitUntil(ctx, target) || !waitForWork(ctx) {
			return attempt, last
		}
		last = time.Now()
		launchAttempt(ctx, candidate, attempt, pending)
	}
	return count, last
}

func (candidate *candidateAttempts) finish(ctx context.Context, launched int) (result Result, rejection string, err error) {
	defer func() {
		if closeErr := candidate.session.Close(); closeErr != nil {
			result = Result{}
			if rejection != "cancelled" {
				rejection = "http-failed"
			}
			err = errors.Join(err, fmt.Errorf("close candidate outbound: %w", closeErr))
		}
	}()
	if ctx.Err() != nil || launched != len(candidate.results) {
		return Result{}, "cancelled", nil
	}
	var httpTotal time.Duration
	httpCount := 0
	var httpError error
	for _, attempt := range candidate.results {
		if attempt.err != nil {
			httpError = attempt.err
			continue
		}
		httpTotal += attempt.delay
		httpCount++
	}
	if httpCount != len(candidate.results) {
		return Result{}, "http-failed", httpError
	}
	return Result{
		IP:         candidate.ip,
		HTTPMillis: durationMillis(httpTotal / time.Duration(httpCount)),
	}, "", nil
}

func topResults(results []Result, count int) []Result {
	if count < len(results) {
		return append([]Result(nil), results[:count]...)
	}
	return results
}

func durationMillis(value time.Duration) float64 {
	return math.Round(float64(value)/float64(time.Millisecond)*100) / 100
}
