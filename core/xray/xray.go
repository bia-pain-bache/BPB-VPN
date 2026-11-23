// Package xray runs Xray-core for the BPB app, bound for Android with gomobile (see
// scripts/build_core.sh): one Xray instance at a time, started from the app's JSON config,
// reading from the TUN device the app's VpnService opened. Only what the app uses: start and
// stop, a delay test through the running instance, the outbound traffic counters, the browser
// dialer address and the version.
//
// The app excludes itself from its VPN, so Xray's own sockets bypass the tunnel without
// VpnService.protect.
package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bpb/core/networking"
	"bpb/core/scanner"
	sshproxy "bpb/core/ssh"
	applog "github.com/xtls/xray-core/app/log"
	commlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	"github.com/xtls/xray-core/transport/internet/browser_dialer"
)

// ScannerBridge adapts the independent scanner API to gomobile's primitive Go types.
// Its JSON methods are transport glue for the Android Flutter platform-channel adapter.
type ScannerBridge struct {
	mu     sync.Mutex
	scan   *scanner.Scanner
	events <-chan scanner.Event
}

// My IP
type MyIp struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code"`
}

func NewScannerBridge() *ScannerBridge { return &ScannerBridge{} }

func (b *ScannerBridge) StartScan(optionsJSON string) string {
	options, err := scanner.ParseOptions(optionsJSON)
	if err != nil {
		return err.Error()
	}
	var probeOptions struct {
		Outbound json.RawMessage `json:"outbound"`
		ProbeURL string          `json:"probeUrl"`
	}
	if err := json.Unmarshal([]byte(optionsJSON), &probeOptions); err != nil {
		return fmt.Sprintf("scanner probe options: %v", err)
	}
	if len(probeOptions.Outbound) == 0 || probeOptions.ProbeURL == "" {
		return "scanner outbound and probe URL are required"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.scan != nil && b.scan.Running() {
		return "scanner is already running"
	}
	probe, err := newScannerProbe()
	if err != nil {
		return err.Error()
	}
	probe.template = probeOptions.Outbound
	probe.url = probeOptions.ProbeURL
	scan := scanner.New(probe)
	if err := scan.Start(context.Background(), options); err != nil {
		return errors.Join(err, probe.Close()).Error()
	}
	b.scan = scan
	b.events = scan.Events()
	return ""
}

func (b *ScannerBridge) PauseScan() string  { return b.controlScan((*scanner.Scanner).Pause) }
func (b *ScannerBridge) ResumeScan() string { return b.controlScan((*scanner.Scanner).Resume) }
func (b *ScannerBridge) StopScan() string   { return b.controlScan((*scanner.Scanner).Stop) }

func (b *ScannerBridge) controlScan(control func(*scanner.Scanner) error) string {
	b.mu.Lock()
	scan := b.scan
	b.mu.Unlock()
	if scan == nil {
		return "scanner is not running"
	}
	if err := control(scan); err != nil {
		return err.Error()
	}
	return ""
}

// PollEvent returns one JSON event, or an empty string after timeout / channel close.
func (b *ScannerBridge) PollEvent(timeoutMillis int) string {
	b.mu.Lock()
	events := b.events
	b.mu.Unlock()
	if events == nil {
		return ""
	}
	if timeoutMillis < 1 {
		timeoutMillis = 1
	}
	if timeoutMillis > 5000 {
		timeoutMillis = 5000
	}
	timer := time.NewTimer(time.Duration(timeoutMillis) * time.Millisecond)
	defer timer.Stop()
	select {
	case event, ok := <-events:
		if !ok {
			b.mu.Lock()
			if b.events == events {
				b.events = nil
			}
			b.mu.Unlock()
			return ""
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return `{"type":"error","message":"could not encode scanner event"}`
		}
		return string(encoded)
	case <-timer.C:
		return ""
	}
}

func (b *ScannerBridge) IsRunning() bool {
	b.mu.Lock()
	scan := b.scan
	b.mu.Unlock()
	return scan != nil && scan.Running()
}

func (b *ScannerBridge) CloseScan() {
	b.mu.Lock()
	scan := b.scan
	b.mu.Unlock()
	if scan != nil && scan.Running() {
		_ = scan.Stop()
	}
}

// Environment variables Xray-core reads.
const (
	envAsset         = "xray.location.asset"
	envCert          = "xray.location.cert"
	envTunFd         = "xray.tun.fd"
	envBrowserDialer = "xray.browser.dialer"
)

// InitEnv points Xray at dir for its geo files (geoip.dat, geosite.dat) and the
// certificates a config names without a path.
func InitEnv(dir string) {
	os.Setenv(envAsset, dir)
	os.Setenv(envCert, dir)
}

// StartEchGateway starts the local ECH gateway for the app's Cloudflare API calls (see
// networking/); it returns an error message, or "" when it runs.
func StartEchGateway() string {
	if err := networking.Start(networking.DefaultAddress); err != nil {
		return err.Error()
	}
	return ""
}

// CheckDNSJSON adapts networking.CheckDNS to gomobile's string-only API.
func CheckDNSJSON(reqJSON string) string {
	var req networking.DNSRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return `{"ok":false}`
	}
	result, err := json.Marshal(networking.CheckDNS(context.Background(), req))
	if err != nil {
		return `{"ok":false}`
	}
	return string(result)
}

// Version is Xray-core's version, e.g. 26.10.21.
func Version() string {
	return core.Version()
}

// SetBrowserDialer sets the browser dialer's listen address, or turns it off ("").
func SetBrowserDialer(address string) {
	os.Setenv(envBrowserDialer, address)
	browser_dialer.Reload()
}

// Controller runs one Xray instance at a time.
type Controller struct {
	mu        sync.Mutex
	instance  *core.Instance
	stats     stats.Manager
	sshConfig string
	sshServer *sshproxy.Server
}

var coreReservation struct {
	sync.Mutex
	owner string
}

func reserveCore(owner string) error {
	coreReservation.Lock()
	defer coreReservation.Unlock()
	if coreReservation.owner != "" {
		return fmt.Errorf("cannot start %s while %s is running", owner, coreReservation.owner)
	}
	coreReservation.owner = owner
	return nil
}

func releaseCore() {
	coreReservation.Lock()
	coreReservation.owner = ""
	coreReservation.Unlock()
}

var registerConsole sync.Once

// NewController sends Xray's console log (when the config names no log file) to stdout.
func NewController() *Controller {
	registerConsole.Do(func() {
		err := applog.RegisterHandlerCreator(applog.LogType_Console,
			func(applog.LogType, applog.HandlerCreatorOptions) (commlog.Handler, error) {
				return commlog.NewLogger(func() commlog.Writer { return stdoutWriter{log.New(os.Stdout, "", 0)} }), nil
			})
		if err != nil {
			log.Printf("console log: %v", err)
		}
	})
	return &Controller{}
}

type stdoutWriter struct{ logger *log.Logger }

func (w stdoutWriter) Write(s string) error {
	w.logger.Print(s)
	return nil
}

func (stdoutWriter) Close() error { return nil }

// IsRunning: an instance is up.
func (c *Controller) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.instance != nil
}

// ConfigureSSH sets or clears the SSH SOCKS settings for the next Start call.
// The service runs in this process and is stopped with the controller.
func (c *Controller) ConfigureSSH(configJSON string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.instance != nil {
		return "cannot change SSH settings while Xray is running"
	}
	c.sshConfig = configJSON
	return ""
}

// Start starts an instance from the JSON config, with the TUN device tunFd (0: none, for proxy
// mode). Does nothing while one is running.
func (c *Controller) Start(config string, tunFd int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.instance != nil {
		return nil
	}
	if err := reserveCore("VPN"); err != nil {
		return err
	}
	started := false
	defer func() {
		if !started {
			releaseCore()
		}
	}()
	os.Setenv(envTunFd, strconv.Itoa(int(tunFd)))

	parsed, err := serial.LoadJSONConfig(strings.NewReader(config))
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	instance, err := core.New(parsed)
	if err != nil {
		return fmt.Errorf("core init failed: %w", err)
	}
	var sshServer *sshproxy.Server
	if c.sshConfig != "" {
		sshServer, err = sshproxy.StartJSON(c.sshConfig)
		if err != nil {
			instance.Close()
			return fmt.Errorf("SSH Failed: %w", err)
		}
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		sshServer.Stop()
		return fmt.Errorf("startup failed: %w", err)
	}
	c.instance = instance
	c.sshServer = sshServer
	c.stats, _ = instance.GetFeature(stats.ManagerType()).(stats.Manager)
	started = true
	return nil
}

// Stop stops the running instance, if any.
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.instance == nil && c.sshServer == nil {
		return nil
	}
	var err error
	if c.instance != nil {
		err = c.instance.Close()
	}
	c.instance, c.stats = nil, nil
	if c.sshServer != nil {
		c.sshServer.Stop()
		c.sshServer = nil
	}
	releaseCore()
	return err
}

// SSHHostKey is the running SSH exit's server key (SHA256:…), pinned or learned on its first
// connection, for the app to remember; "" when there is no SSH exit or no key yet.
func (c *Controller) SSHHostKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sshServer.HostKey()
}

// OutboundTraffic reads and resets the outbound traffic counters (the config's policy turns
// them on): "tag,uplink|downlink,bytes;" for each counter above 0.
func (c *Controller) OutboundTraffic() string {
	c.mu.Lock()
	manager := c.stats
	c.mu.Unlock()
	if manager == nil {
		return ""
	}
	var b strings.Builder
	manager.VisitCounters(func(name string, counter stats.Counter) bool {
		// outbound>>>tag>>>traffic>>>uplink
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[0] != "outbound" || parts[2] != "traffic" {
			return true
		}
		if bytes := counter.Set(0); bytes > 0 {
			fmt.Fprintf(&b, "%s,%s,%d;", parts[1], parts[3], bytes)
		}
		return true
	})
	return b.String()
}

// MeasureDelay is the time in ms for a GET of url (a 204 / 200 page) through the running
// instance, the faster of two tries on one connection, so the second skips the handshakes.
func (c *Controller) MeasureDelay(url string) (int64, error) {
	c.mu.Lock()
	instance := c.instance
	c.mu.Unlock()
	if instance == nil {
		return -1, errors.New("not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	transport := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, instance, dest)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	best := int64(-1)
	var lastErr error
	for range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return -1, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent:
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
		case err != nil:
			lastErr = err
		default:
			if ms := time.Since(start).Milliseconds(); best < 0 || ms < best {
				best = ms
			}
		}
	}
	if best < 0 {
		return -1, lastErr
	}
	return best, nil
}

// Fetch IP from different endpoints for CF and non-CF addresses
func (c *Controller) GetMyIp(url string) (*MyIp, error) {
	c.mu.Lock()
	instance := c.instance
	c.mu.Unlock()
	if instance == nil {
		return nil, errors.New("not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	transport := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, instance, dest)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := range 2 {
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
		} else {
			if resp.StatusCode == http.StatusOK {
				var myIp MyIp
				err := json.NewDecoder(resp.Body).Decode(&myIp)
				resp.Body.Close()
				if err != nil {
					return nil, err
				}
				// Return as a struct object for standard gomobile interop
				return &myIp, nil
			}

			status := resp.Status
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil, fmt.Errorf("HTTP %s", status)
			}
			lastErr = fmt.Errorf("HTTP %s", status)
		}
		if attempt == 1 {
			break
		}

		delay := 500 * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("request failed after 2 attempts: %w", lastErr)
}
