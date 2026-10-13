package checks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// CheckTLS establishes a TCP connection to a literal IP address and port,
// then performs a TLS handshake while verifying the certificate against
// the original hostname.
//
// A nil roots pool uses the system certificate pool. Certificate
// verification is never disabled.
//
// The TLS connection is independent of any TCP connectivity check.
// Invalid input and dependencies are returned as Go errors. Operational
// outcomes are represented as diagnostic results.
func CheckTLS(
	ctx context.Context,
	dialer Dialer,
	address netip.Addr,
	hostname string,
	port int,
	timeout time.Duration,
	roots *x509.CertPool,
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

	if hostname == "" {
		return diagnostic.Result{}, errors.New("hostname must not be empty")
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

	result := diagnostic.Result{
		Name:     "tls",
		Required: true,
		Details: map[string]any{
			"address":    address.String(),
			"hostname":   hostname,
			"port":       port,
			"timeout_ms": timeout.Milliseconds(),
			"network":    "tcp",
			"min_tls":    "1.2",
		},
	}

	conn, err := dialer.DialContext(
		attemptCtx,
		"tcp",
		target,
	)
	if err != nil {
		result.Duration = time.Since(startedAt)
		result.Error = err.Error()

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			ctx.Err() != nil ||
			attemptCtx.Err() != nil {
			result.Status = diagnostic.StatusInconclusive
			result.Message = "TLS connection did not complete within the available context"
		} else {
			result.Status = diagnostic.StatusFailed
			result.Message = "TLS connection failed"
		}

		return result, nil
	}

	if conn == nil {
		result.Duration = time.Since(startedAt)
		result.Status = diagnostic.StatusFailed
		result.Message = "TLS dialer returned no connection"
		result.Error = "dialer returned a nil connection without an error"

		return result, nil
	}

	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: hostname,
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	})

	err = tlsConn.HandshakeContext(attemptCtx)
	result.Duration = time.Since(startedAt)

	if err != nil {
		result.Error = err.Error()

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			ctx.Err() != nil ||
			attemptCtx.Err() != nil {
			result.Status = diagnostic.StatusInconclusive
			result.Message = "TLS handshake did not complete within the available context"
		} else {
			result.Status = diagnostic.StatusFailed
			result.Message = "TLS handshake or certificate verification failed"
		}

		return result, nil
	}

	state := tlsConn.ConnectionState()

	result.Status = diagnostic.StatusPassed
	result.Message = "TLS handshake and certificate verification succeeded"
	result.Details["tls_version"] = tlsVersionName(state.Version)
	result.Details["cipher_suite"] = tls.CipherSuiteName(state.CipherSuite)
	result.Details["verified_chains"] = len(state.VerifiedChains)

	return result, nil
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "1.0"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS13:
		return "1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
}
