package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"bpb/scanner"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/infra/conf/serial"
)

type scannerProbe struct {
	instance *core.Instance
	manager  outbound.Manager
	nextTag  atomic.Uint64
	template json.RawMessage
	url      string
}

func newScannerProbe() (*scannerProbe, error) {
	if err := reserveCore("scanner"); err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			releaseCore()
		}
	}()
	config, err := serial.LoadJSONConfig(strings.NewReader(scannerBaseConfig))
	if err != nil {
		return nil, fmt.Errorf("parse scanner config: %w", err)
	}
	instance, err := core.New(config)
	if err != nil {
		return nil, fmt.Errorf("create scanner instance: %w", err)
	}
	if err := instance.Start(); err != nil {
		return nil, errors.Join(fmt.Errorf("start scanner instance: %w", err), instance.Close())
	}
	manager, ok := instance.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if !ok {
		return nil, errors.Join(errors.New("scanner outbound manager unavailable"), instance.Close())
	}
	started = true
	return &scannerProbe{instance: instance, manager: manager}, nil
}

func (p *scannerProbe) close() error {
	err := p.instance.Close()
	releaseCore()
	return err
}

func (p *scannerProbe) Close() error {
	return p.close()
}

type scannerSession struct {
	probe   *scannerProbe
	tag     string
	handler outbound.Handler
}

func (p *scannerProbe) Open(ctx context.Context, address string) (scanner.HTTPSession, error) {
	return p.open(ctx, address)
}

func (p *scannerProbe) open(ctx context.Context, address string) (scanner.HTTPSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(p.template, &config); err != nil {
		return nil, fmt.Errorf("parse scanner outbound: %w", err)
	}
	if config == nil {
		return nil, errors.New("scanner outbound must be an object")
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(config["settings"], &settings); err != nil || settings == nil {
		return nil, errors.New("scanner outbound settings must be an object")
	}
	{
		var protocol string
		if err := json.Unmarshal(config["protocol"], &protocol); err != nil {
			return nil, fmt.Errorf("parse scanner protocol: %w", err)
		}
		if protocol == "wireguard" {
			var peers []map[string]json.RawMessage
			if err := json.Unmarshal(settings["peers"], &peers); err != nil || len(peers) != 1 {
				return nil, errors.New("scanner WireGuard outbound must have exactly one peer")
			}
			var endpoint string
			if err := json.Unmarshal(peers[0]["endpoint"], &endpoint); err != nil {
				return nil, fmt.Errorf("parse scanner WireGuard endpoint: %w", err)
			}
			_, port, err := net.SplitHostPort(endpoint)
			if err != nil {
				return nil, fmt.Errorf("parse scanner WireGuard endpoint: %w", err)
			}
			peers[0]["endpoint"], err = json.Marshal(net.JoinHostPort(address, port))
			if err != nil {
				return nil, fmt.Errorf("encode scanner WireGuard endpoint: %w", err)
			}
			settings["peers"], err = json.Marshal(peers)
			if err != nil {
				return nil, fmt.Errorf("encode scanner WireGuard peer: %w", err)
			}
		} else {
			encodedAddress, err := json.Marshal(address)
			if err != nil {
				return nil, fmt.Errorf("encode scanner address: %w", err)
			}
			settings["address"] = encodedAddress
		}
	}
	var err error
	config["settings"], err = json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("encode scanner settings: %w", err)
	}
	tag := fmt.Sprintf("scanner-%d", p.nextTag.Add(1))
	config["tag"], err = json.Marshal(tag)
	if err != nil {
		return nil, fmt.Errorf("encode scanner tag: %w", err)
	}
	raw, err := json.Marshal(map[string]any{"outbounds": []map[string]json.RawMessage{config}})
	if err != nil {
		return nil, fmt.Errorf("encode scanner outbound: %w", err)
	}
	parsed, err := serial.LoadJSONConfig(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("build scanner outbound: %w", err)
	}
	if err := core.AddOutboundHandler(p.instance, parsed.Outbound[0]); err != nil {
		if handler := p.manager.GetHandler(tag); handler != nil {
			return nil, errors.Join(fmt.Errorf("add scanner outbound: %w", err), p.manager.RemoveHandler(context.Background(), tag), handler.Close())
		}
		return nil, fmt.Errorf("add scanner outbound: %w", err)
	}
	handler := p.manager.GetHandler(tag)
	if handler == nil {
		return nil, errors.New("added scanner outbound not found")
	}
	return &scannerSession{probe: p, tag: tag, handler: handler}, nil
}

func (s *scannerSession) Close() error {
	return errors.Join(s.probe.manager.RemoveHandler(context.Background(), s.tag), s.handler.Close())
}

// Ping performs the same two GETs as Controller.MeasureDelay.
func (s *scannerSession) Ping(ctx context.Context) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	transport := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			ctx = session.ContextWithContent(ctx, &session.Content{})
			ctx = session.SetForcedOutboundTagToContext(ctx, s.tag)
			return core.Dial(ctx, s.probe.instance, dest)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	var best time.Duration
	var lastErr error
	succeeded := false
	for range 2 {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, s.probe.url, nil)
		if reqErr != nil {
			return 0, reqErr
		}
		start := time.Now()
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		switch {
		case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent:
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
		case readErr != nil:
			lastErr = readErr
		case closeErr != nil:
			lastErr = closeErr
		default:
			elapsed := time.Since(start)
			if !succeeded || elapsed < best {
				best = elapsed
			}
			succeeded = true
		}
	}
	if !succeeded {
		return 0, lastErr
	}
	return best, nil
}
