package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// Dialer abstracts TCP connection establishment so the check can be
// tested without depending on live network services.
type Dialer interface {
	DialContext(
		ctx context.Context,
		network string,
		address string,
	) (net.Conn, error)
}

// CheckTCP attempts to establish a TCP connection to a literal IP address
// and port.
//
// The timeout bounds the connection attempt without extending an earlier
// deadline imposed by the parent context.
//
// Invalid input and invalid dependencies are returned as Go errors.
// Connection outcomes are represented as diagnostic results.
func CheckTCP(
	ctx context.Context,
	dialer Dialer,
	address netip.Addr,
	port int,
	timeout time.Duration,
) (diagnostic.Result, error) {
	if ctx == nil {
		return diagnostic.Result{}, errors.New("context must not be nil")
	}

	if dialer == nil {
		return diagnostic.Result{}, errors.New("dialer must not be nil")
	}

	if !address.IsValid() {
		return diagnostic.Result{}, errors.New("IP address must be valid")
	}

	if port < 1 || port > 65535 {
		return diagnostic.Result{}, fmt.Errorf(
			"port must be between 1 and 65535: %d",
			port,
		)
	}

	if timeout <= 0 {
		return diagnostic.Result{}, errors.New(
			"timeout must be greater than zero",
		)
	}

	address = address.Unmap()

	target := net.JoinHostPort(
		address.String(),
		strconv.Itoa(port),
	)

	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startedAt := time.Now()

	conn, err := dialer.DialContext(
		attemptCtx,
		"tcp",
		target,
	)

	duration := time.Since(startedAt)

	result := diagnostic.Result{
		Name:     "tcp",
		Required: true,
		Duration: duration,
		Details: map[string]any{
			"address":    address.String(),
			"port":       port,
			"timeout_ms": timeout.Milliseconds(),
			"network":    "tcp",
		},
	}

	if err != nil {
		result.Error = err.Error()

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			ctx.Err() != nil ||
			attemptCtx.Err() != nil {
			result.Status = diagnostic.StatusInconclusive
			result.Message = "TCP connection attempt did not complete within the available context"
		} else {
			result.Status = diagnostic.StatusFailed
			result.Message = "TCP connection attempt failed"
		}

		return result, nil
	}

	if conn == nil {
		result.Status = diagnostic.StatusFailed
		result.Message = "TCP dialer returned no connection"
		result.Error = "dialer returned a nil connection without an error"

		return result, nil
	}

	// Reachability has been established. NetProbe does not retain the
	// connection after the diagnostic check completes.
	if err := conn.Close(); err != nil {
		result.Details["close_error"] = err.Error()
	}

	result.Status = diagnostic.StatusPassed
	result.Message = "TCP connection established successfully"

	return result, nil
}
