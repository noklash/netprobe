package runner

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/noklash/netprobe/internal/checks"
	"github.com/noklash/netprobe/internal/diagnostic"
)

// Config defines the target and dependencies for one diagnostic run.
type Config struct {
	Target string
	Port   int

	DNSTimeout time.Duration
	TCPTimeout time.Duration
	TLSTimeout time.Duration

	// EnableTLS enables TLS handshake and certificate verification
	// after successful TCP connectivity checks.
	//
	// TLS is disabled by default because not every TCP service
	// speaks TLS.
	EnableTLS bool

	// TLSRoots optionally supplies trusted certificate authorities.
	//
	// When nil, Go uses the system certificate pool.
	TLSRoots *x509.CertPool

	Resolver checks.Resolver
	Dialer   checks.Dialer
}

// Run resolves the target and performs the configured diagnostic checks.
//
// Configuration errors are returned as Go errors. Operational network
// outcomes are represented as diagnostic results in the returned report.
//
// DNS is checked once. When DNS succeeds, each returned IP address is
// checked independently. A failure against one address does not prevent
// attempts against remaining addresses unless the parent context is
// canceled.
//
// When TLS is enabled, TLS is attempted only after the TCP check for the
// corresponding address succeeds. TLS certificate verification uses the
// original target hostname, not the resolved IP address.
func Run(
	ctx context.Context,
	config Config,
) (diagnostic.Report, error) {
	if ctx == nil {
		return diagnostic.Report{}, errors.New(
			"context must not be nil",
		)
	}

	if config.Target == "" {
		return diagnostic.Report{}, errors.New(
			"target must not be empty",
		)
	}

	if config.Port < 1 || config.Port > 65535 {
		return diagnostic.Report{}, fmt.Errorf(
			"port must be between 1 and 65535: %d",
			config.Port,
		)
	}

	if config.DNSTimeout <= 0 {
		return diagnostic.Report{}, errors.New(
			"DNS timeout must be greater than zero",
		)
	}

	if config.TCPTimeout <= 0 {
		return diagnostic.Report{}, errors.New(
			"TCP timeout must be greater than zero",
		)
	}

	if config.EnableTLS && config.TLSTimeout <= 0 {
		return diagnostic.Report{}, errors.New(
			"TLS timeout must be greater than zero when TLS is enabled",
		)
	}

	if config.Resolver == nil {
		return diagnostic.Report{}, errors.New(
			"resolver must not be nil",
		)
	}

	if config.Dialer == nil {
		return diagnostic.Report{}, errors.New(
			"dialer must not be nil",
		)
	}

	startedAt := time.Now()
	results := make([]diagnostic.Result, 0)

	// Bound DNS resolution without extending an earlier parent deadline.
	dnsCtx, cancelDNS := context.WithTimeout(
		ctx,
		config.DNSTimeout,
	)

	dnsResult, addresses, err := checks.ResolveDNS(
		dnsCtx,
		config.Resolver,
		config.Target,
	)

	cancelDNS()

	if err != nil {
		return diagnostic.Report{}, fmt.Errorf(
			"resolve DNS for %q: %w",
			config.Target,
			err,
		)
	}

	results = append(results, dnsResult)

	// Without a successful DNS result and usable addresses, neither
	// TCP nor TLS can be attempted meaningfully.
	if dnsResult.Status != diagnostic.StatusPassed ||
		len(addresses) == 0 {
		results = append(results, skippedTCPResult(
			config,
			"",
			"TCP check skipped because DNS did not produce usable addresses",
		))

		if config.EnableTLS {
			results = append(results, skippedTLSResult(
				config,
				"",
				"TLS check skipped because DNS did not produce usable addresses",
			))
		}

		return newReport(
			config.Target,
			startedAt,
			results,
		)
	}

	for _, address := range addresses {
		// Preserve explicit results for checks that cannot be attempted
		// after cancellation or expiration of the parent context.
		if ctx.Err() != nil {
			results = append(results, skippedTCPResult(
				config,
				address.String(),
				"TCP check skipped because the diagnostic context was canceled or expired",
			))

			if config.EnableTLS {
				results = append(results, skippedTLSResult(
					config,
					address.String(),
					"TLS check skipped because the diagnostic context was canceled or expired",
				))
			}

			continue
		}

		tcpResult, err := checks.CheckTCP(
			ctx,
			config.Dialer,
			address,
			config.Port,
			config.TCPTimeout,
		)

		if err != nil {
			return diagnostic.Report{}, fmt.Errorf(
				"check TCP connectivity to %s:%d: %w",
				address,
				config.Port,
				err,
			)
		}

		results = append(results, tcpResult)

		// Preserve existing behavior when TLS is not enabled.
		if !config.EnableTLS {
			continue
		}

		// A TLS handshake should only be attempted after TCP
		// connectivity has been established for this address.
		if tcpResult.Status != diagnostic.StatusPassed {
			results = append(results, skippedTLSResult(
				config,
				address.String(),
				fmt.Sprintf(
					"TLS check skipped because the TCP check was %s",
					tcpResult.Status,
				),
			))

			continue
		}

		// CheckTLS opens its own connection. The TLS observation is
		// therefore independent of the preceding TCP observation.
		//
		// The original hostname is retained for certificate
		// verification even though the connection targets an IP.
		tlsResult, err := checks.CheckTLS(
			ctx,
			config.Dialer,
			address,
			config.Target,
			config.Port,
			config.TLSTimeout,
			config.TLSRoots,
		)

		if err != nil {
			return diagnostic.Report{}, fmt.Errorf(
				"check TLS for %s at %s:%d: %w",
				config.Target,
				address,
				config.Port,
				err,
			)
		}

		results = append(results, tlsResult)
	}

	return newReport(
		config.Target,
		startedAt,
		results,
	)
}

// skippedTCPResult records a TCP check that could not be attempted.
func skippedTCPResult(
	config Config,
	address string,
	reason string,
) diagnostic.Result {
	details := map[string]any{
		"port":        config.Port,
		"timeout_ms":  config.TCPTimeout.Milliseconds(),
		"network":     "tcp",
		"skip_reason": reason,
	}

	if address != "" {
		details["address"] = address
	}

	return diagnostic.Result{
		Name:     "tcp",
		Status:   diagnostic.StatusSkipped,
		Required: true,
		Message:  reason,
		Details:  details,
	}
}

// skippedTLSResult records a TLS check that could not be attempted.
func skippedTLSResult(
	config Config,
	address string,
	reason string,
) diagnostic.Result {
	details := map[string]any{
		"hostname":    config.Target,
		"port":        config.Port,
		"timeout_ms":  config.TLSTimeout.Milliseconds(),
		"network":     "tcp",
		"skip_reason": reason,
	}

	if address != "" {
		details["address"] = address
	}

	return diagnostic.Result{
		Name:     "tls",
		Status:   diagnostic.StatusSkipped,
		Required: true,
		Message:  reason,
		Details:  details,
	}
}

// newReport validates and aggregates the results collected by the runner.
func newReport(
	target string,
	startedAt time.Time,
	results []diagnostic.Result,
) (diagnostic.Report, error) {
	report, err := diagnostic.NewReport(
		target,
		startedAt,
		time.Since(startedAt),
		results,
	)

	if err != nil {
		return diagnostic.Report{}, fmt.Errorf(
			"build diagnostic report: %w",
			err,
		)
	}

	return report, nil
}

// ensure the netip import remains tied to the address-oriented runner API.
