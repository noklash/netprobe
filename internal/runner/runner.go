package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/noklash/netprobe/internal/checks"
	"github.com/noklash/netprobe/internal/diagnostic"
)

// Config defines the target and dependencies for one diagnostic run.
type Config struct {
	Target     string
	Port       int
	DNSTimeout time.Duration
	TCPTimeout time.Duration
	Resolver   checks.Resolver
	Dialer     checks.Dialer
}

// Run resolves the target and checks TCP connectivity to each resolved IP.
//
// Configuration errors are returned as Go errors. Network outcomes are
// preserved as diagnostic results in the returned report.
//
// DNS is checked once. When DNS succeeds, each returned IP address is checked
// independently. A failure against one address does not prevent attempts
// against the remaining addresses unless the parent context is canceled.
func Run(ctx context.Context, config Config) (diagnostic.Report, error) {
	if ctx == nil {
		return diagnostic.Report{}, errors.New("context must not be nil")
	}

	if config.Target == "" {
		return diagnostic.Report{}, errors.New("target must not be empty")
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

	if config.Resolver == nil {
		return diagnostic.Report{}, errors.New("resolver must not be nil")
	}

	if config.Dialer == nil {
		return diagnostic.Report{}, errors.New("dialer must not be nil")
	}

	startedAt := time.Now()
	results := make([]diagnostic.Result, 0)

	// Bound DNS resolution without extending an earlier parent deadline.
	dnsCtx, cancelDNS := context.WithTimeout(ctx, config.DNSTimeout)

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

	if dnsResult.Status != diagnostic.StatusPassed || len(addresses) == 0 {
		results = append(results, skippedTCPResult(
			config,
			"",
			"TCP check skipped because DNS did not produce usable addresses",
		))

		return newReport(config.Target, startedAt, results)
	}

	for _, address := range addresses {
		// Preserve an explicit result for every address we did not attempt.
		// This makes cancellation visible rather than silently truncating
		// the report.
		if ctx.Err() != nil {
			results = append(results, skippedTCPResult(
				config,
				address.String(),
				"TCP check skipped because the diagnostic context was canceled or expired",
			))
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
	}

	return newReport(config.Target, startedAt, results)
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
