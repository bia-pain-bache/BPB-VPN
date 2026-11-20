package xray

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func testScannerPing(probe *scannerProbe, ctx context.Context, address string) (time.Duration, error) {
	session, err := probe.Open(ctx, address)
	if err != nil {
		return 0, err
	}
	delay, err := session.Ping(ctx)
	return delay, errors.Join(err, session.Close())
}

func TestScannerConcurrentPingsReuseHandler(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	probe.template = json.RawMessage(`{"protocol":"freedom","settings":{}}`)
	probe.url = server.URL
	session, err := probe.Open(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var pending sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		pending.Add(1)
		go func() {
			defer pending.Done()
			_, err := session.Ping(ctx)
			errs <- err
		}()
	}
	for range 3 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("concurrent pings did not reach the server")
		}
	}
	if probe.nextTag.Load() != 1 || len(probe.manager.ListHandlers(ctx)) != 1 {
		t.Fatal("concurrent attempts did not reuse one outbound")
	}
	close(release)
	pending.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if len(probe.manager.ListHandlers(ctx)) != 0 {
		t.Fatal("outbound was not removed")
	}
}

func TestScannerTenPingsReuseOneOutbound(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	probe.template = json.RawMessage(`{"protocol":"freedom","settings":{}}`)
	probe.url = server.URL
	session, err := probe.Open(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := session.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
		if probe.nextTag.Load() != 1 || len(probe.manager.ListHandlers(context.Background())) != 1 {
			t.Fatal("baseline did not reuse one outbound")
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if len(probe.manager.ListHandlers(context.Background())) != 0 {
		t.Fatal("baseline outbound was not removed")
	}
}
