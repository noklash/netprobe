package runner

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

	// EnableHTTP enables an independent HTTP response check.
	//
	// The HTTP request uses HTTPURL directly. It does not reuse the
	// connections or resolved addresses from the DNS/TCP/TLS checks.
	EnableHTTP bool

	// HTTPURL is the absolute URL to request when HTTP is enabled.
	//
	// The URL must use HTTP or HTTPS and include a hostname.
	HTTPURL string

	// HTTPExpectedStatus is the exact HTTP status code expected
	// from the configured endpoint.
	HTTPExpectedStatus int

	// HTTPTimeout bounds the HTTP request.
	HTTPTimeout time.Duration

	// HTTPClient optionally supplies the HTTP client used by the
	// HTTP check. When nil, http.DefaultClient is used.
	//
	// The HTTP check clones the client to enforce its redirect
	// policy without changing the supplied client.
	HTTPClient *http.Client

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
// When TLS is enabled, TLS is attempted only after the TCP check for
// the corresponding address succeeds. TLS certificate verification uses
// the original target hostname, not the resolved IP address.
//
// When HTTP is enabled, one independent HTTP request is made against
// HTTPURL. It does not depend on the DNS, TCP, or TLS outcomes above.
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

	if config.EnableHTTP {
		if err := validateHTTPConfig(config); err != nil {
			return diagnostic.Report{}, err
		}
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

	// Without a successful DNS result and usable addresses, dependent
	// TCP and TLS checks cannot proceed. An enabled HTTP check remains
	// independent and must still be attempted.
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

		if config.EnableHTTP {
			if err := appendHTTPResult(ctx, config, &results); err != nil {
				return diagnostic.Report{}, err
			}
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

	// HTTP is evaluated once, separately from the per-address checks.
	// Its result is retained even if a TCP or TLS check failed.
	if config.EnableHTTP {
		if err := appendHTTPResult(ctx, config, &results); err != nil {
			return diagnostic.Report{}, err
		}
	}

	return newReport(
		config.Target,
		startedAt,
		results,
	)
}

// validateHTTPConfig validates HTTP configuration before any network
// operations begin.
func validateHTTPConfig(config Config) error {
	parsedURL, err := url.Parse(config.HTTPURL)
	if err != nil {
		return fmt.Errorf("invalid HTTP URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New(
			"HTTP URL scheme must be http or https when HTTP is enabled",
		)
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return errors.New(
			"HTTP URL must include a hostname when HTTP is enabled",
		)
	}

	if parsedURL.User != nil {
		return errors.New(
			"HTTP URL must not contain user information",
		)
	}

	if config.HTTPExpectedStatus < 100 ||
		config.HTTPExpectedStatus > 599 {
		return fmt.Errorf(
			"HTTP expected status must be between 100 and 599: %d",
			config.HTTPExpectedStatus,
		)
	}

	if config.HTTPTimeout <= 0 {
		return errors.New(
			"HTTP timeout must be greater than zero when HTTP is enabled",
		)
	}

	return nil
}

// appendHTTPResult performs the independent HTTP check and appends its
// diagnostic result to the report results.
func appendHTTPResult(
	ctx context.Context,
	config Config,
	results *[]diagnostic.Result,
) error {
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	httpResult, err := checks.CheckHTTP(
		ctx,
		client,
		config.HTTPURL,
		config.HTTPExpectedStatus,
		config.HTTPTimeout,
	)
	if err != nil {
		return fmt.Errorf("run HTTP diagnostic: %w", err)
	}

	*results = append(*results, httpResult)

	return nil
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
