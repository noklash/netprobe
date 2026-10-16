package runner

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// fakeResolver provides deterministic DNS results for runner tests.
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

// fakeDialer records connection attempts and allows specific outcomes.
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

// checksDialer represents the connection dependency used by the runner.
// It allows the test helper to accept both fake and real dialers.
type checksDialer interface {
	DialContext(
		context.Context,
		string,
		string,
	) (net.Conn, error)
}

func testConfig(
	resolver *fakeResolver,
	dialer checksDialer,
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

	report, err := Run(
		context.Background(),
		testConfig(resolver, dialer),
	)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if report.Status != diagnostic.StatusFailed {
		t.Fatalf(
			"report status = %q, want %q",
			report.Status,
			diagnostic.StatusFailed,
		)
	}

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Errorf(
			"first result name = %q, want dns",
			report.Results[0].Name,
		)
	}

	if report.Results[0].Status != diagnostic.StatusFailed {
		t.Errorf(
			"DNS status = %q, want failed",
			report.Results[0].Status,
		)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TCP status = %q, want skipped",
			report.Results[1].Status,
		)
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

	report, err := Run(
		context.Background(),
		testConfig(resolver, dialer),
	)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	wantAttempts := []string{
		"192.0.2.10:443",
		"[2001:db8::10]:443",
	}

	if !reflect.DeepEqual(dialer.attempts, wantAttempts) {
		t.Errorf(
			"TCP attempts = %v, want %v",
			dialer.attempts,
			wantAttempts,
		)
	}

	if len(report.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(report.Results))
	}

	if report.Results[0].Name != "dns" {
		t.Errorf(
			"first result = %q, want dns",
			report.Results[0].Name,
		)
	}

	if report.Results[1].Status != diagnostic.StatusFailed {
		t.Errorf(
			"first TCP status = %q, want failed",
			report.Results[1].Status,
		)
	}

	if report.Results[2].Status != diagnostic.StatusPassed {
		t.Errorf(
			"second TCP status = %q, want passed",
			report.Results[2].Status,
		)
	}

	// Every TCP attempt is required. One failed address therefore
	// makes the overall report fail, even if another address succeeds.
	if report.Status != diagnostic.StatusFailed {
		t.Errorf(
			"report status = %q, want failed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunNoUsableAddressesSkipsTCP(t *testing.T) {
	resolver := &fakeResolver{}
	dialer := &fakeDialer{}

	report, err := Run(
		context.Background(),
		testConfig(resolver, dialer),
	)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(report.Results))
	}

	if report.Results[0].Status != diagnostic.StatusFailed {
		t.Errorf(
			"DNS status = %q, want failed",
			report.Results[0].Status,
		)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TCP status = %q, want skipped",
			report.Results[1].Status,
		)
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
		t.Errorf(
			"first result = %q, want dns",
			report.Results[0].Name,
		)
	}

	if report.Results[1].Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"first TCP status = %q, want inconclusive",
			report.Results[1].Status,
		)
	}

	if report.Results[2].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"second TCP status = %q, want skipped",
			report.Results[2].Status,
		)
	}

	if len(dialer.attempts) != 1 {
		t.Errorf(
			"got %d TCP attempts, want 1",
			len(dialer.attempts),
		)
	}

	if report.Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"report status = %q, want inconclusive",
			report.Status,
		)
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
		{
			name: "TLS enabled with zero timeout",
			change: func(c *Config) {
				c.EnableTLS = true
				c.TLSTimeout = 0
			},
		},
		{
			name: "TLS enabled with negative timeout",
			change: func(c *Config) {
				c.EnableTLS = true
				c.TLSTimeout = -time.Second
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testConfig(validResolver, validDialer)
			tt.change(&config)

			_, err := Run(context.Background(), config)
			if err == nil {
				t.Fatal(
					"Run() error = nil, want configuration error",
				)
			}
		})
	}
}

func TestRunRejectsNilContext(t *testing.T) {
	resolver := &fakeResolver{}
	dialer := &fakeDialer{}
	config := testConfig(resolver, dialer)

	//lint:ignore SA1012 Passing nil is intentional: this test verifies
	// that Run rejects a nil context.
	_, err := Run(nil, config)
	if err == nil {
		t.Fatal("Run() expected an error for nil context")
	}

	if !strings.Contains(strings.ToLower(err.Error()), "context") {
		t.Fatalf(
			"Run() error = %q, want an error mentioning context",
			err,
		)
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
		t.Errorf(
			"skip reason = %v, want explanation of DNS failure",
			result.Details["skip_reason"],
		)
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
		t.Errorf(
			"first result = %q, want dns",
			report.Results[0].Name,
		)
	}

	if report.Results[0].Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"DNS status = %q, want inconclusive",
			report.Results[0].Status,
		)
	}

	if report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TCP status = %q, want skipped",
			report.Results[1].Status,
		)
	}

	if len(dialer.attempts) != 0 {
		t.Errorf("TCP attempts = %v, want none", dialer.attempts)
	}

	if report.Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"report status = %q, want inconclusive",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

// runnerTLSFixture provides a local TLS server and a certificate pool
// that trusts its self-signed test certificate.
type runnerTLSFixture struct {
	address netip.Addr
	port    int
	roots   *x509.CertPool
}

// startRunnerTLSFixture starts a local TLS server.
//
// The first accepted connection is used by the TCP check. The second
// connection is used by the TLS handshake check.
func startRunnerTLSFixture(t *testing.T) runnerTLSFixture {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "example.test",
		},
		DNSNames: []string{
			"example.test",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certificate := tls.Certificate{
		Certificate: [][]byte{certificateDER},
		PrivateKey:  privateKey,
	}

	parsedCertificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(parsedCertificate)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start TLS fixture listener: %v", err)
	}

	t.Cleanup(func() {
		_ = listener.Close()
	})

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split fixture address: %v", err)
	}

	address, err := netip.ParseAddr(host)
	if err != nil {
		t.Fatalf("parse fixture IP address: %v", err)
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse fixture port: %v", err)
	}

	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			if attempt == 0 {
				_ = conn.Close()
				continue
			}

			server := tls.Server(conn, &tls.Config{
				Certificates: []tls.Certificate{certificate},
				MinVersion:   tls.VersionTLS12,
			})

			_ = server.Handshake()
			_ = server.Close()
		}
	}()

	return runnerTLSFixture{
		address: address,
		port:    port,
		roots:   roots,
	}
}

func TestRunTLSAfterSuccessfulTCP(t *testing.T) {
	fixture := startRunnerTLSFixture(t)

	resolver := &fakeResolver{
		addresses: []netip.Addr{fixture.address},
	}

	dialer := &net.Dialer{}

	config := testConfig(resolver, dialer)
	config.Port = fixture.port
	config.EnableTLS = true
	config.TLSTimeout = 2 * time.Second
	config.TLSRoots = fixture.roots

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP, TLS)",
			len(report.Results),
		)
	}

	if report.Results[0].Name != "dns" {
		t.Errorf(
			"first result name = %q, want dns",
			report.Results[0].Name,
		)
	}

	if report.Results[0].Status != diagnostic.StatusPassed {
		t.Errorf(
			"DNS status = %q, want passed",
			report.Results[0].Status,
		)
	}

	if report.Results[1].Name != "tcp" {
		t.Errorf(
			"second result name = %q, want tcp",
			report.Results[1].Name,
		)
	}

	if report.Results[1].Status != diagnostic.StatusPassed {
		t.Errorf(
			"TCP status = %q, want passed",
			report.Results[1].Status,
		)
	}

	if report.Results[2].Name != "tls" {
		t.Errorf(
			"third result name = %q, want tls",
			report.Results[2].Name,
		)
	}

	if report.Results[2].Status != diagnostic.StatusPassed {
		t.Errorf(
			"TLS status = %q, want passed; message: %s",
			report.Results[2].Status,
			report.Results[2].Message,
		)
	}

	if report.Results[2].Details["hostname"] != config.Target {
		t.Errorf(
			"TLS hostname = %v, want %q",
			report.Results[2].Details["hostname"],
			config.Target,
		)
	}

	if report.Status != diagnostic.StatusPassed {
		t.Errorf(
			"report status = %q, want passed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunSkipsTLSAfterTCPFailure(t *testing.T) {
	address := netip.MustParseAddr("192.0.2.10")

	resolver := &fakeResolver{
		addresses: []netip.Addr{address},
	}

	dialer := &fakeDialer{
		outcomes: map[string]error{
			"192.0.2.10:443": errors.New("connection refused"),
		},
	}

	config := testConfig(resolver, dialer)
	config.EnableTLS = true
	config.TLSTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP, TLS)",
			len(report.Results),
		)
	}

	tcpResult := report.Results[1]
	tlsResult := report.Results[2]

	if tcpResult.Name != "tcp" {
		t.Errorf("second result = %q, want tcp", tcpResult.Name)
	}

	if tcpResult.Status != diagnostic.StatusFailed {
		t.Errorf(
			"TCP status = %q, want failed",
			tcpResult.Status,
		)
	}

	if tlsResult.Name != "tls" {
		t.Errorf("third result = %q, want tls", tlsResult.Name)
	}

	if tlsResult.Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TLS status = %q, want skipped",
			tlsResult.Status,
		)
	}

	reason, ok := tlsResult.Details["skip_reason"].(string)
	if !ok || !strings.Contains(reason, "failed") {
		t.Errorf(
			"TLS skip reason = %v, want explanation mentioning TCP failure",
			tlsResult.Details["skip_reason"],
		)
	}

	if len(dialer.attempts) != 1 {
		t.Errorf(
			"got %d dial attempts, want 1; TLS must not be attempted",
			len(dialer.attempts),
		)
	}

	if report.Status != diagnostic.StatusFailed {
		t.Errorf(
			"report status = %q, want failed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunTLSDisabledDoesNotRequireTimeout(t *testing.T) {
	config := testConfig(
		&fakeResolver{},
		&fakeDialer{},
	)

	config.EnableTLS = false
	config.TLSTimeout = 0

	_, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf(
			"Run() returned unexpected error with TLS disabled: %v",
			err,
		)
	}
}

func TestRunDNSFailureSkipsTLSWhenEnabled(t *testing.T) {
	resolver := &fakeResolver{
		err: errors.New("DNS server unavailable"),
	}

	dialer := &fakeDialer{}

	config := testConfig(resolver, dialer)
	config.EnableTLS = true
	config.TLSTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP skipped, TLS skipped)",
			len(report.Results),
		)
	}

	if report.Results[1].Name != "tcp" ||
		report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TCP result = (%q, %q), want (tcp, skipped)",
			report.Results[1].Name,
			report.Results[1].Status,
		)
	}

	if report.Results[2].Name != "tls" ||
		report.Results[2].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TLS result = (%q, %q), want (tls, skipped)",
			report.Results[2].Name,
			report.Results[2].Status,
		)
	}

	if len(dialer.attempts) != 0 {
		t.Errorf(
			"got %d dial attempts, want none",
			len(dialer.attempts),
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

// -----------------------------------------------------------------------------
// HTTP runner tests
// -----------------------------------------------------------------------------

func TestRunHTTPCheckPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("HTTP method = %q, want GET", r.Method)
			}

			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	resolver := &fakeResolver{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
		},
	}
	dialer := &fakeDialer{}

	config := testConfig(resolver, dialer)
	config.EnableHTTP = true
	config.HTTPURL = server.URL
	config.HTTPExpectedStatus = http.StatusOK
	config.HTTPTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP, HTTP)",
			len(report.Results),
		)
	}

	httpResult := report.Results[2]

	if httpResult.Name != "http" {
		t.Errorf(
			"third result name = %q, want http",
			httpResult.Name,
		)
	}

	if httpResult.Status != diagnostic.StatusPassed {
		t.Errorf(
			"HTTP status = %q, want passed; message: %s",
			httpResult.Status,
			httpResult.Message,
		)
	}

	if report.Status != diagnostic.StatusPassed {
		t.Errorf(
			"report status = %q, want passed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunHTTPCheckFailsOnUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	))
	defer server.Close()

	resolver := &fakeResolver{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
		},
	}
	dialer := &fakeDialer{}

	config := testConfig(resolver, dialer)
	config.EnableHTTP = true
	config.HTTPURL = server.URL
	config.HTTPExpectedStatus = http.StatusOK
	config.HTTPTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP, HTTP)",
			len(report.Results),
		)
	}

	httpResult := report.Results[2]

	if httpResult.Name != "http" {
		t.Errorf(
			"third result name = %q, want http",
			httpResult.Name,
		)
	}

	if httpResult.Status != diagnostic.StatusFailed {
		t.Errorf(
			"HTTP status = %q, want failed",
			httpResult.Status,
		)
	}

	if report.Status != diagnostic.StatusFailed {
		t.Errorf(
			"report status = %q, want failed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunHTTPCheckRunsAfterDNSFailure(t *testing.T) {
	requestReceived := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			select {
			case requestReceived <- struct{}{}:
			default:
			}

			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	resolver := &fakeResolver{
		err: errors.New("DNS server unavailable"),
	}
	dialer := &fakeDialer{}

	config := testConfig(resolver, dialer)
	config.EnableHTTP = true
	config.HTTPURL = server.URL
	config.HTTPExpectedStatus = http.StatusOK
	config.HTTPTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	select {
	case <-requestReceived:
		// The independent HTTP request was received.
	default:
		t.Fatal("HTTP request was not received after DNS failure")
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP skipped, HTTP)",
			len(report.Results),
		)
	}

	if report.Results[0].Name != "dns" ||
		report.Results[0].Status != diagnostic.StatusFailed {
		t.Errorf(
			"DNS result = (%q, %q), want (dns, failed)",
			report.Results[0].Name,
			report.Results[0].Status,
		)
	}

	if report.Results[1].Name != "tcp" ||
		report.Results[1].Status != diagnostic.StatusSkipped {
		t.Errorf(
			"TCP result = (%q, %q), want (tcp, skipped)",
			report.Results[1].Name,
			report.Results[1].Status,
		)
	}

	if report.Results[2].Name != "http" ||
		report.Results[2].Status != diagnostic.StatusPassed {
		t.Errorf(
			"HTTP result = (%q, %q), want (http, passed)",
			report.Results[2].Name,
			report.Results[2].Status,
		)
	}

	// A passing HTTP endpoint does not erase the DNS failure.
	if report.Status != diagnostic.StatusFailed {
		t.Errorf(
			"report status = %q, want failed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunHTTPCheckRunsAfterTCPFailure(t *testing.T) {
	requestReceived := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			select {
			case requestReceived <- struct{}{}:
			default:
			}

			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	resolver := &fakeResolver{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
		},
	}

	dialer := &fakeDialer{
		outcomes: map[string]error{
			"192.0.2.10:443": errors.New("connection refused"),
		},
	}

	config := testConfig(resolver, dialer)
	config.EnableHTTP = true
	config.HTTPURL = server.URL
	config.HTTPExpectedStatus = http.StatusOK
	config.HTTPTimeout = time.Second

	report, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	select {
	case <-requestReceived:
		// The independent HTTP request was received.
	default:
		t.Fatal("HTTP request was not received after TCP failure")
	}

	if len(report.Results) != 3 {
		t.Fatalf(
			"got %d results, want 3 (DNS, TCP, HTTP)",
			len(report.Results),
		)
	}

	if report.Results[1].Name != "tcp" ||
		report.Results[1].Status != diagnostic.StatusFailed {
		t.Errorf(
			"TCP result = (%q, %q), want (tcp, failed)",
			report.Results[1].Name,
			report.Results[1].Status,
		)
	}

	if report.Results[2].Name != "http" ||
		report.Results[2].Status != diagnostic.StatusPassed {
		t.Errorf(
			"HTTP result = (%q, %q), want (http, passed)",
			report.Results[2].Name,
			report.Results[2].Status,
		)
	}

	// A passing HTTP endpoint does not erase the TCP failure.
	if report.Status != diagnostic.StatusFailed {
		t.Errorf(
			"report status = %q, want failed",
			report.Status,
		)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation failed: %v", err)
	}
}

func TestRunRejectsInvalidHTTPConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{
			name: "empty URL",
			change: func(c *Config) {
				c.HTTPURL = ""
			},
		},
		{
			name: "unsupported URL scheme",
			change: func(c *Config) {
				c.HTTPURL = "ftp://example.test"
			},
		},
		{
			name: "missing URL host",
			change: func(c *Config) {
				c.HTTPURL = "http:///path"
			},
		},
		{
			name: "zero HTTP timeout",
			change: func(c *Config) {
				c.HTTPTimeout = 0
			},
		},
		{
			name: "negative HTTP timeout",
			change: func(c *Config) {
				c.HTTPTimeout = -time.Second
			},
		},
		{
			name: "invalid expected status below range",
			change: func(c *Config) {
				c.HTTPExpectedStatus = 99
			},
		},
		{
			name: "invalid expected status above range",
			change: func(c *Config) {
				c.HTTPExpectedStatus = 600
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeResolver{}
			dialer := &fakeDialer{}

			config := testConfig(resolver, dialer)
			config.EnableHTTP = true
			config.HTTPURL = "http://example.test"
			config.HTTPExpectedStatus = http.StatusOK
			config.HTTPTimeout = time.Second

			tt.change(&config)

			_, err := Run(context.Background(), config)
			if err == nil {
				t.Fatal(
					"Run() error = nil, want HTTP configuration error",
				)
			}

			if resolver.calls != 0 {
				t.Errorf(
					"resolver calls = %d, want 0 for invalid configuration",
					resolver.calls,
				)
			}

			if len(dialer.attempts) != 0 {
				t.Errorf(
					"dial attempts = %v, want none for invalid configuration",
					dialer.attempts,
				)
			}
		})
	}
}

func TestRunHTTPDisabledDoesNotRequireConfiguration(t *testing.T) {
	config := testConfig(
		&fakeResolver{},
		&fakeDialer{},
	)

	config.EnableHTTP = false
	config.HTTPURL = ""
	config.HTTPExpectedStatus = 0
	config.HTTPTimeout = 0
	config.HTTPClient = nil

	_, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf(
			"Run() returned unexpected error with HTTP disabled: %v",
			err,
		)
	}
}
