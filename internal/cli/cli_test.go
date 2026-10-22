package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

type fixedResolver struct {
	addresses []netip.Addr
	err       error
}

func (r fixedResolver) LookupNetIP(
	_ context.Context,
	_ string,
	_ string,
) ([]netip.Addr, error) {
	if r.err != nil {
		return nil, r.err
	}

	return append([]netip.Addr(nil), r.addresses...), nil
}

type failingDialer struct {
	err error
}

func (d failingDialer) DialContext(
	_ context.Context,
	_ string,
	_ string,
) (net.Conn, error) {
	if d.err != nil {
		return nil, d.err
	}

	return nil, errors.New("simulated connection refusal")
}

func testDependencies() dependencies {
	return dependencies{
		resolver: fixedResolver{
			addresses: []netip.Addr{
				netip.MustParseAddr("127.0.0.1"),
			},
		},
		dialer: failingDialer{
			err: errors.New("simulated connection refusal"),
		},
	}
}

func runForTest(
	args ...string,
) (int, string, string) {
	var stdout strings.Builder
	var stderr strings.Builder

	code := runWithDependencies(
		context.Background(),
		args,
		&stdout,
		&stderr,
		testDependencies(),
	)

	return code, stdout.String(), stderr.String()
}

func TestRunRequiresTarget(t *testing.T) {
	code, stdout, stderr := runForTest()

	if code != diagnostic.ExitCodeInvalidArguments {
		t.Fatalf(
			"expected exit code %d, got %d",
			diagnostic.ExitCodeInvalidArguments,
			code,
		)
	}

	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}

	if !strings.Contains(stderr, "-target") {
		t.Fatalf("expected missing-target error, got %q", stderr)
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "invalid port",
			args: []string{
				"-target", "example.com",
				"-port", "70000",
			},
			want: "port must be between",
		},
		{
			name: "invalid format",
			args: []string{
				"-target", "example.com",
				"-format", "yaml",
			},
			want: "unsupported output format",
		},
		{
			name: "invalid DNS timeout",
			args: []string{
				"-target", "example.com",
				"-dns-timeout", "0s",
			},
			want: "DNS timeout must be greater than zero",
		},
		{
			name: "invalid TCP timeout",
			args: []string{
				"-target", "example.com",
				"-tcp-timeout", "-1s",
			},
			want: "TCP timeout must be greater than zero",
		},
		{
			name: "invalid TLS timeout when TLS enabled",
			args: []string{
				"-target", "example.com",
				"-tls",
				"-tls-timeout", "0s",
			},
			want: "TLS timeout must be greater than zero",
		},
		{
			name: "invalid HTTP scheme",
			args: []string{
				"-target", "example.com",
				"-http-url", "ftp://example.com",
			},
			want: "HTTP URL scheme must be http or https",
		},
		{
			name: "HTTP URL missing hostname",
			args: []string{
				"-target", "example.com",
				"-http-url", "https:///health",
			},
			want: "HTTP URL must include a hostname",
		},
		{
			name: "HTTP URL contains user information",
			args: []string{
				"-target", "example.com",
				"-http-url", "https://user:password@example.com/health",
			},
			want: "must not contain user information",
		},
		{
			name: "invalid HTTP status",
			args: []string{
				"-target", "example.com",
				"-http-url", "https://example.com/health",
				"-http-status", "99",
			},
			want: "HTTP expected status must be between",
		},
		{
			name: "invalid HTTP timeout",
			args: []string{
				"-target", "example.com",
				"-http-url", "https://example.com/health",
				"-http-timeout", "0s",
			},
			want: "HTTP timeout must be greater than zero",
		},
		{
			name: "unexpected positional argument",
			args: []string{
				"-target", "example.com",
				"extra",
			},
			want: "unexpected positional arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runForTest(tt.args...)

			if code != diagnostic.ExitCodeInvalidArguments {
				t.Fatalf(
					"expected exit code %d, got %d; stderr=%q",
					diagnostic.ExitCodeInvalidArguments,
					code,
					stderr,
				)
			}

			if !strings.Contains(stderr, tt.want) {
				t.Fatalf(
					"expected stderr to contain %q, got %q",
					tt.want,
					stderr,
				)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	code, _, stderr := runForTest("-h")

	if code != diagnostic.ExitCodeSuccess {
		t.Fatalf(
			"expected help exit code %d, got %d",
			diagnostic.ExitCodeSuccess,
			code,
		)
	}

	if !strings.Contains(stderr, "Usage: netprobe") {
		t.Fatalf("expected usage information, got %q", stderr)
	}

	if !strings.Contains(stderr, "-target") {
		t.Fatalf("expected target flag in help output, got %q", stderr)
	}
}

func TestRunJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	deps := dependencies{
		resolver: fixedResolver{
			addresses: []netip.Addr{
				netip.MustParseAddr("127.0.0.1"),
			},
		},
		dialer: failingDialer{
			err: errors.New("simulated TCP failure"),
		},
	}

	var stdout strings.Builder
	var stderr strings.Builder

	code := runWithDependencies(
		context.Background(),
		[]string{
			"-target", "example.com",
			"-http-url", server.URL,
			"-format", "json",
		},
		&stdout,
		&stderr,
		deps,
	)

	if code != diagnostic.ExitCodeDiagnosticFailure {
		t.Fatalf(
			"expected diagnostic failure exit code %d, got %d; stderr=%q",
			diagnostic.ExitCodeDiagnosticFailure,
			code,
			stderr.String(),
		)
	}

	var report diagnostic.Report

	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("expected valid JSON report: %v\noutput: %s", err, stdout.String())
	}

	if report.Target != "example.com" {
		t.Fatalf("expected target example.com, got %q", report.Target)
	}

	if report.Status != diagnostic.StatusFailed {
		t.Fatalf("expected failed report status, got %q", report.Status)
	}

	var foundHTTP bool

	for _, result := range report.Results {
		if result.Name == "http" {
			foundHTTP = true

			if result.Status != diagnostic.StatusPassed {
				t.Fatalf(
					"expected HTTP check to pass, got %q",
					result.Status,
				)
			}
		}
	}

	if !foundHTTP {
		t.Fatal("expected HTTP result in report")
	}
}

func TestRunTextOutput(t *testing.T) {
	code, stdout, _ := runForTest(
		"-target", "example.com",
	)

	if code != diagnostic.ExitCodeDiagnosticFailure {
		t.Fatalf(
			"expected diagnostic failure exit code %d, got %d",
			diagnostic.ExitCodeDiagnosticFailure,
			code,
		)
	}

	for _, want := range []string{
		"NetProbe diagnostic report",
		"Target: example.com",
		"Status: FAILED",
		"[PASSED] dns",
		"[FAILED] tcp",
		"Required: true",
		"simulated connection refusal",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf(
				"expected output to contain %q, got:\n%s",
				want,
				stdout,
			)
		}
	}
}

func TestRunReturnsDiagnosticFailureExitCode(t *testing.T) {
	code, _, _ := runForTest(
		"-target", "example.com",
	)

	if code != diagnostic.ExitCodeDiagnosticFailure {
		t.Fatalf(
			"expected exit code %d, got %d",
			diagnostic.ExitCodeDiagnosticFailure,
			code,
		)
	}
}

func TestRunUsesConfiguredTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	deps := dependencies{
		resolver: fixedResolver{
			addresses: []netip.Addr{
				netip.MustParseAddr("127.0.0.1"),
			},
		},
		dialer: failingDialer{
			err: errors.New("simulated TCP failure"),
		},
	}

	var stdout strings.Builder
	var stderr strings.Builder

	_ = runWithDependencies(
		context.Background(),
		[]string{
			"-target", "example.com",
			"-tcp-timeout", "250ms",
			"-http-url", server.URL,
			"-http-timeout", "1500ms",
			"-format", "json",
		},
		&stdout,
		&stderr,
		deps,
	)

	var report diagnostic.Report

	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("expected valid JSON report: %v\noutput: %s", err, stdout.String())
	}

	var foundTCP bool
	var foundHTTP bool

	for _, result := range report.Results {
		switch result.Name {
		case "tcp":
			foundTCP = true

			if got := result.Details["timeout_ms"]; got != int64(250) &&
				got != float64(250) {
				t.Errorf("expected TCP timeout of 250ms, got %#v", got)
			}

		case "http":
			foundHTTP = true

			if got := result.Details["timeout_ms"]; got != int64(1500) &&
				got != float64(1500) {
				t.Errorf("expected HTTP timeout of 1500ms, got %#v", got)
			}
		}
	}

	if !foundTCP {
		t.Fatal("expected TCP result")
	}

	if !foundHTTP {
		t.Fatal("expected HTTP result")
	}
}

func TestRunRejectsNilContext(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	code := runWithDependencies(
		nil,
		[]string{"-target", "example.com"},
		&stdout,
		&stderr,
		testDependencies(),
	)

	if code != diagnostic.ExitCodeInternalError {
		t.Fatalf(
			"expected internal error exit code %d, got %d; stderr=%q",
			diagnostic.ExitCodeInternalError,
			code,
			stderr.String(),
		)
	}

	if !strings.Contains(stderr.String(), "context must not be nil") {
		t.Fatalf(
			"expected nil-context error, got %q",
			stderr.String(),
		)
	}
}

func TestRunRejectsNilDependencies(t *testing.T) {
	tests := []struct {
		name string
		deps dependencies
		want string
	}{
		{
			name: "nil resolver",
			deps: dependencies{
				dialer: failingDialer{},
			},
			want: "resolver must not be nil",
		},
		{
			name: "nil dialer",
			deps: dependencies{
				resolver: fixedResolver{
					addresses: []netip.Addr{
						netip.MustParseAddr("127.0.0.1"),
					},
				},
			},
			want: "dialer must not be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout strings.Builder
			var stderr strings.Builder

			code := runWithDependencies(
				context.Background(),
				[]string{"-target", "example.com"},
				&stdout,
				&stderr,
				tt.deps,
			)

			if code != diagnostic.ExitCodeInternalError {
				t.Fatalf(
					"expected exit code %d, got %d",
					diagnostic.ExitCodeInternalError,
					code,
				)
			}

			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf(
					"expected error to contain %q, got %q",
					tt.want,
					stderr.String(),
				)
			}
		})
	}
}

func TestRunRejectsNilWriters(t *testing.T) {
	t.Run("nil stdout", func(t *testing.T) {
		var stderr strings.Builder

		code := runWithDependencies(
			context.Background(),
			[]string{"-target", "example.com"},
			nil,
			&stderr,
			testDependencies(),
		)

		if code != diagnostic.ExitCodeInternalError {
			t.Fatalf(
				"expected exit code %d, got %d",
				diagnostic.ExitCodeInternalError,
				code,
			)
		}
	})

	t.Run("nil stderr", func(t *testing.T) {
		code := runWithDependencies(
			context.Background(),
			[]string{"-target", "example.com"},
			io.Discard,
			nil,
			testDependencies(),
		)

		if code != diagnostic.ExitCodeInternalError {
			t.Fatalf(
				"expected exit code %d, got %d",
				diagnostic.ExitCodeInternalError,
				code,
			)
		}
	})
}

func TestValidateOptionsAcceptsHTTPURL(t *testing.T) {
	opts := options{
		target:             "example.com",
		port:               443,
		dnsTimeout:         2 * time.Second,
		tcpTimeout:         2 * time.Second,
		httpURL:            "https://example.com/health",
		httpExpectedStatus: http.StatusOK,
		httpTimeout:        3 * time.Second,
		format:             "json",
	}

	if err := validateOptions(opts); err != nil {
		t.Fatalf("expected valid options, got %v", err)
	}
}
