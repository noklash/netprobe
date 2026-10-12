package checks

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

type dialerFunc func(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error)

func (f dialerFunc) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestCheckTCPSuccess(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()

	var gotNetwork string
	var gotAddress string

	dialer := dialerFunc(func(
		_ context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		gotNetwork = network
		gotAddress = address
		return client, nil
	})

	result, err := CheckTCP(
		context.Background(),
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf("expected passed status, got %q", result.Status)
	}

	if result.Name != "tcp" {
		t.Errorf("expected result name tcp, got %q", result.Name)
	}

	if result.Duration < 0 {
		t.Errorf("duration must not be negative: %v", result.Duration)
	}

	if gotNetwork != "tcp" {
		t.Errorf("expected network tcp, got %q", gotNetwork)
	}

	if gotAddress != "192.0.2.10:443" {
		t.Errorf("unexpected dial address: %q", gotAddress)
	}

	if result.Details["address"] != "192.0.2.10" {
		t.Errorf("unexpected address detail: %v", result.Details["address"])
	}

	if result.Details["port"] != 443 {
		t.Errorf("unexpected port detail: %v", result.Details["port"])
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}

	// The connection must be closed before CheckTCP returns.
	if _, err := client.Write([]byte("test")); err == nil {
		t.Error("expected the successful connection to be closed")
	}
}

func TestCheckTCPIPv6AddressFormatting(t *testing.T) {
	dialer := dialerFunc(func(
		_ context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		if network != "tcp" {
			t.Errorf("expected network tcp, got %q", network)
		}

		if address != "[2001:db8::1]:443" {
			t.Errorf("unexpected IPv6 dial address: %q", address)
		}

		return nil, errors.New("simulated connection failure")
	})

	result, err := CheckTCP(
		context.Background(),
		dialer,
		netip.MustParseAddr("2001:db8::1"),
		443,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Errorf("expected failed status, got %q", result.Status)
	}
}

func TestCheckTCPConnectionFailure(t *testing.T) {
	wantErr := errors.New("connection refused")

	dialer := dialerFunc(func(
		_ context.Context,
		_, _ string,
	) (net.Conn, error) {
		return nil, wantErr
	})

	result, err := CheckTCP(
		context.Background(),
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Errorf("expected failed status, got %q", result.Status)
	}

	if result.Error != wantErr.Error() {
		t.Errorf("expected error %q, got %q", wantErr, result.Error)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckTCPTimeout(t *testing.T) {
	dialer := dialerFunc(func(
		ctx context.Context,
		_, _ string,
	) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	result, err := CheckTCP(
		context.Background(),
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"expected inconclusive status, got %q",
			result.Status,
		)
	}

	if result.Error != context.DeadlineExceeded.Error() {
		t.Errorf("unexpected error: %q", result.Error)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckTCPParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dialer := dialerFunc(func(
		ctx context.Context,
		_, _ string,
	) (net.Conn, error) {
		return nil, ctx.Err()
	})

	result, err := CheckTCP(
		ctx,
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"expected inconclusive status, got %q",
			result.Status,
		)
	}
}

func TestCheckTCPInvalidInput(t *testing.T) {
	validDialer := dialerFunc(func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		return nil, errors.New("dialer should not be called")
	})

	tests := []struct {
		name    string
		ctx     context.Context
		dialer  Dialer
		address netip.Addr
		port    int
		timeout time.Duration
	}{
		{
			name:    "nil context",
			ctx:     nil,
			dialer:  validDialer,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    443,
			timeout: time.Second,
		},
		{
			name:    "nil dialer",
			ctx:     context.Background(),
			dialer:  nil,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    443,
			timeout: time.Second,
		},
		{
			name:    "invalid address",
			ctx:     context.Background(),
			dialer:  validDialer,
			address: netip.Addr{},
			port:    443,
			timeout: time.Second,
		},
		{
			name:    "port zero",
			ctx:     context.Background(),
			dialer:  validDialer,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    0,
			timeout: time.Second,
		},
		{
			name:    "port too large",
			ctx:     context.Background(),
			dialer:  validDialer,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    65536,
			timeout: time.Second,
		},
		{
			name:    "zero timeout",
			ctx:     context.Background(),
			dialer:  validDialer,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    443,
			timeout: 0,
		},
		{
			name:    "negative timeout",
			ctx:     context.Background(),
			dialer:  validDialer,
			address: netip.MustParseAddr("192.0.2.10"),
			port:    443,
			timeout: -time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CheckTCP(
				tt.ctx,
				tt.dialer,
				tt.address,
				tt.port,
				tt.timeout,
			)

			if err == nil {
				t.Fatal("expected an input validation error")
			}

			if result.Name != "" {
				t.Errorf(
					"expected an empty result for invalid input, got %q",
					result.Name,
				)
			}
		})
	}
}

func TestCheckTCPNilConnectionWithoutError(t *testing.T) {
	dialer := dialerFunc(func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		return nil, nil
	})

	result, err := CheckTCP(
		context.Background(),
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Errorf("expected failed status, got %q", result.Status)
	}

	if result.Error == "" {
		t.Error("expected an explanatory error")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckTCPParentDeadlineTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	dialer := dialerFunc(func(
		ctx context.Context,
		_, _ string,
	) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	startedAt := time.Now()

	result, err := CheckTCP(
		ctx,
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		443,
		5*time.Second,
	)

	elapsed := time.Since(startedAt)

	if err != nil {
		t.Fatalf("CheckTCP returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Fatalf(
			"expected inconclusive status, got %q",
			result.Status,
		)
	}

	if elapsed >= time.Second {
		t.Fatalf(
			"parent deadline was not respected promptly; elapsed %v",
			elapsed,
		)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}
