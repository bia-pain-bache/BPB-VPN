package xray

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
)

func TestScannerBaseConfig(t *testing.T) {
	config, err := serial.LoadJSONConfig(strings.NewReader(scannerBaseConfig))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Inbound) != 0 || len(config.Outbound) != 0 {
		t.Fatalf("base config must have no inbounds or outbounds: %d inbounds, %d outbounds", len(config.Inbound), len(config.Outbound))
	}
	instance, err := core.New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ping := func(tag string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if tag != "" {
			ctx = session.SetForcedOutboundTagToContext(ctx, tag)
		}
		transport := &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dest, err := xnet.ParseDestination(network + ":" + addr)
				if err != nil {
					return nil, err
				}
				return core.Dial(ctx, instance, dest)
			},
		}
		defer transport.CloseIdleConnections()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			return err
		}
		resp, err := (&http.Client{Transport: transport}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusNoContent {
			return errors.New("unexpected HTTP status: " + resp.Status)
		}
		return nil
	}

	if err := ping(""); err == nil {
		t.Fatal("request without an outbound unexpectedly succeeded")
	}
	if err := ping("missing"); err == nil {
		t.Fatal("request with a missing outbound unexpectedly succeeded")
	}

	outboundConfig, err := serial.LoadJSONConfig(strings.NewReader(`{"outbounds":[{"tag":"candidate","protocol":"freedom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := core.AddOutboundHandler(instance, outboundConfig.Outbound[0]); err != nil {
		t.Fatal(err)
	}
	if err := ping("candidate"); err != nil {
		t.Fatalf("request through added outbound: %v", err)
	}
	if err := ping("missing"); err == nil {
		t.Fatal("request with a missing tag fell back to the default outbound")
	}
}
