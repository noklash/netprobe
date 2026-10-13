package runner

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

type fakeResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
}

func (f *fakeResolver) LookupNetIP(
	ctx context.Context,
	network string,
	host string,
) ([]netip.Addr, error) {
	f.calls++

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if f.err != nil {
		return nil, f.err
	}

	return append([]netip.Addr(nil), f.addresses...), nil
}

type fakeDialer struct {
	attempts []string
	outcomes map[string]error
	onDial   func(context.Context, string)
}

func (f *fakeDialer) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	f.attempts = append(f.attempts, address)

	if f.onDial != nil {
		f.onDial(ctx, address)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := f.outcomes[address]; err != nil {
		return nil, err
	}

	client, peer := net.Pipe()
	_ = peer.Close()

	return client, nil
}

func testConfig(
	resolver *fakeResolver,
	dialer *fakeDialer,
) Config {
	return Config{
		Target:     "example.test",
		Port:       443,
		DNSTimeout: time.Second,
		TCPTimeout: time.Second,
		Resolver:   resolver,
		Dialer:     dialer,
	}
}

func TestRunDNSFailureSkipsTCP(t *testing.T) {
	resolver := &fakeResolver{
		err: errors.New("DNS server unavailable"),
	}
	dialer := &fakeDialer{}

	report, err := Run(context.Background(), testConfig(resolver, dialer))
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if report.Status != diagnostic.StatusFailed {
		t.Fatalf("report status = %q, want %q",
			report.Status, diagnostic.StatusFailed)
	}

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Fatalf("first result name = %q, want dns",
			report.Results[0].Name)
	}

	if report.Results[0].Status != diagnostic.StatusFailed {
		t.Errorf("DNS status = %q, want failed",
			report.Results[0].Status)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf("TCP status = %q, want skipped",
			report.Results[1].Status)
	}

	if len(dialer.attempts) != 0 {
		t.Errorf("TCP attempts = %v, want none", dialer.attempts)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunChecksEveryResolvedAddress(t *testing.T) {
	ipv4 := netip.MustParseAddr("192.0.2.10")
	ipv6 := netip.MustParseAddr("2001:db8::10")

	resolver := &fakeResolver{
		addresses: []netip.Addr{ipv6, ipv4},
	}

	dialer := &fakeDialer{
		outcomes: map[string]error{
			"192.0.2.10:443": errors.New("connection refused"),
		},
	}

	report, err := Run(context.Background(), testConfig(resolver, dialer))
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	wantAttempts := []string{
		"192.0.2.10:443",
		"[2001:db8::10]:443",
	}

	if !reflect.DeepEqual(dialer.attempts, wantAttempts) {
		t.Errorf("TCP attempts = %v, want %v",
			dialer.attempts, wantAttempts)
	}

	if len(report.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Errorf("first result = %q, want dns", report.Results[0].Name)
	}

	if report.Results[1].Status != diagnostic.StatusFailed {
		t.Errorf("first TCP status = %q, want failed",
			report.Results[1].Status)
	}

	if report.Results[2].Status != diagnostic.StatusPassed {
		t.Errorf("second TCP status = %q, want passed",
			report.Results[2].Status)
	}

	// The current aggregation policy treats every TCP attempt as required.
	// Therefore, one failed address makes the overall report fail.
	if report.Status != diagnostic.StatusFailed {
		t.Errorf("report status = %q, want failed",
			report.Status)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunNoUsableAddressesSkipsTCP(t *testing.T) {
	resolver := &fakeResolver{}
	dialer := &fakeDialer{}

	report, err := Run(context.Background(), testConfig(resolver, dialer))
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(report.Results))
	}

	if report.Results[0].Status != diagnostic.StatusFailed {
		t.Errorf("DNS status = %q, want failed",
			report.Results[0].Status)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf("TCP status = %q, want skipped",
			report.Results[1].Status)
	}

	if len(dialer.attempts) != 0 {
		t.Errorf("TCP attempts = %v, want none", dialer.attempts)
	}
}

func TestRunCancellationPreservesPartialResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := &fakeResolver{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
			netip.MustParseAddr("192.0.2.20"),
		},
	}

	dialer := &fakeDialer{
		onDial: func(_ context.Context, _ string) {
			cancel()
		},
	}

	report, err := Run(ctx, testConfig(resolver, dialer))
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Errorf("first result = %q, want dns", report.Results[0].Name)
	}

	if report.Results[1].Status != diagnostic.StatusInconclusive {
		t.Errorf("first TCP status = %q, want inconclusive",
			report.Results[1].Status)
	}

	if report.Results[2].Status != diagnostic.StatusSkipped {
		t.Errorf("second TCP status = %q, want skipped",
			report.Results[2].Status)
	}

	if len(dialer.attempts) != 1 {
		t.Errorf("got %d TCP attempts, want 1", len(dialer.attempts))
	}

	if report.Status != diagnostic.StatusInconclusive {
		t.Errorf("report status = %q, want inconclusive",
			report.Status)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	validResolver := &fakeResolver{}
	validDialer := &fakeDialer{}

	tests := []struct {
		name   string
		change func(*Config)
	}{
		{
			name: "empty target",
			change: func(c *Config) {
				c.Target = ""
			},
		},
		{
			name: "invalid port",
			change: func(c *Config) {
				c.Port = 65536
			},
		},
		{
			name: "zero DNS timeout",
			change: func(c *Config) {
				c.DNSTimeout = 0
			},
		},
		{
			name: "negative TCP timeout",
			change: func(c *Config) {
				c.TCPTimeout = -time.Second
			},
		},
		{
			name: "nil resolver",
			change: func(c *Config) {
				c.Resolver = nil
			},
		},
		{
			name: "nil dialer",
			change: func(c *Config) {
				c.Dialer = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testConfig(validResolver, validDialer)
			tt.change(&config)

			_, err := Run(context.Background(), config)
			if err == nil {
				t.Fatal("Run() error = nil, want configuration error")
			}
		})
	}
}

func TestRunRejectsNilContext(t *testing.T) {
	config := testConfig(&fakeResolver{}, &fakeDialer{})

	_, err := Run(nil, config)
	if err == nil {
		t.Fatal("Run(nil, config) error = nil, want error")
	}
}

func TestSkippedTCPResultExplainsReason(t *testing.T) {
	config := testConfig(&fakeResolver{}, &fakeDialer{})

	result := skippedTCPResult(
		config,
		"192.0.2.10",
		"TCP check skipped because DNS failed",
	)

	if result.Status != diagnostic.StatusSkipped {
		t.Fatalf("status = %q, want skipped", result.Status)
	}

	reason, ok := result.Details["skip_reason"].(string)
	if !ok || !strings.Contains(reason, "DNS failed") {
		t.Errorf("skip reason = %v, want explanation of DNS failure",
			result.Details["skip_reason"])
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestRunDNSCancellationPreservesDiagnosticResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := &fakeResolver{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
		},
	}
	dialer := &fakeDialer{}

	report, err := Run(ctx, testConfig(resolver, dialer))
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Errorf("first result = %q, want dns", report.Results[0].Name)
	}

	if report.Results[0].Status != diagnostic.StatusInconclusive {
		t.Errorf("DNS status = %q, want inconclusive",
			report.Results[0].Status)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf("TCP status = %q, want skipped",
			report.Results[1].Status)
	}

	if len(dialer.attempts) != 0 {
		t.Errorf("TCP attempts = %v, want none", dialer.attempts)
	}

	if report.Status != diagnostic.StatusInconclusive {
		t.Errorf("report status = %q, want inconclusive",
			report.Status)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}
