package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/noklash/netprobe/internal/checks"
	"github.com/noklash/netprobe/internal/diagnostic"
	"github.com/noklash/netprobe/internal/runner"
)

const (
	defaultPort         = 443
	defaultDNSTimeout   = 2 * time.Second
	defaultTCPTimeout   = 2 * time.Second
	defaultTLSTimeout   = 3 * time.Second
	defaultHTTPTimeout  = 3 * time.Second
	defaultHTTPStatus   = 200
	defaultOutputFormat = "text"
)

type options struct {
	target             string
	port               int
	dnsTimeout         time.Duration
	tcpTimeout         time.Duration
	enableTLS          bool
	tlsTimeout         time.Duration
	httpURL            string
	httpExpectedStatus int
	httpTimeout        time.Duration
	format             string
}

type dependencies struct {
	resolver checks.Resolver
	dialer   checks.Dialer
}

// Run parses the command-line arguments, executes the configured
// diagnostics, writes the report, and returns the appropriate exit code.
//
// Exit codes:
//   - 0: diagnostic succeeded, or help was requested
//   - 1: one or more required diagnostic checks did not pass
//   - 2: invalid command-line arguments or configuration
//   - 3: an internal error prevented a valid report from being produced
func Run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	return runWithDependencies(
		ctx,
		args,
		stdout,
		stderr,
		dependencies{
			resolver: net.DefaultResolver,
			dialer:   &net.Dialer{},
		},
	)
}

func runWithDependencies(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	deps dependencies,
) int {
	if ctx == nil {
		fmt.Fprintln(stderr, "netprobe: context must not be nil")
		return diagnostic.ExitCodeInternalError
	}

	if stdout == nil {
		fmt.Fprintln(stderr, "netprobe: stdout must not be nil")
		return diagnostic.ExitCodeInternalError
	}

	if stderr == nil {
		return diagnostic.ExitCodeInternalError
	}

	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return diagnostic.ExitCodeSuccess
		}

		return diagnostic.ExitCodeInvalidArguments
	}

	if err := validateOptions(opts); err != nil {
		fmt.Fprintf(stderr, "netprobe: %v\n", err)
		return diagnostic.ExitCodeInvalidArguments
	}

	if deps.resolver == nil {
		fmt.Fprintln(stderr, "netprobe: resolver must not be nil")
		return diagnostic.ExitCodeInternalError
	}

	if deps.dialer == nil {
		fmt.Fprintln(stderr, "netprobe: dialer must not be nil")
		return diagnostic.ExitCodeInternalError
	}

	config := runner.Config{
		Target:     opts.target,
		Port:       opts.port,
		DNSTimeout: opts.dnsTimeout,
		TCPTimeout: opts.tcpTimeout,
		EnableTLS:  opts.enableTLS,
		TLSTimeout: opts.tlsTimeout,
		Resolver:   deps.resolver,
		Dialer:     deps.dialer,
	}

	// Supplying an HTTP URL enables the independent HTTP diagnostic.
	// HTTP does not depend on the DNS, TCP, or TLS check results.
	if opts.httpURL != "" {
		config.EnableHTTP = true
		config.HTTPURL = opts.httpURL
		config.HTTPExpectedStatus = opts.httpExpectedStatus
		config.HTTPTimeout = opts.httpTimeout
	}

	report, err := runner.Run(ctx, config)
	if err != nil {
		fmt.Fprintf(stderr, "netprobe: diagnostic could not run: %v\n", err)
		return diagnostic.ExitCodeInternalError
	}

	if err := writeReport(stdout, report, opts.format); err != nil {
		fmt.Fprintf(stderr, "netprobe: could not write report: %v\n", err)
		return diagnostic.ExitCodeInternalError
	}

	return report.ExitCode()
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options

	flags := flag.NewFlagSet("netprobe", flag.ContinueOnError)
	flags.SetOutput(stderr)

	flags.StringVar(
		&opts.target,
		"target",
		"",
		"Hostname to diagnose (required)",
	)

	flags.IntVar(
		&opts.port,
		"port",
		defaultPort,
		"TCP port to check",
	)

	flags.DurationVar(
		&opts.dnsTimeout,
		"dns-timeout",
		defaultDNSTimeout,
		"Maximum duration for DNS resolution",
	)

	flags.DurationVar(
		&opts.tcpTimeout,
		"tcp-timeout",
		defaultTCPTimeout,
		"Maximum duration for each TCP connection attempt",
	)

	flags.BoolVar(
		&opts.enableTLS,
		"tls",
		false,
		"Enable TLS handshake and certificate verification",
	)

	flags.DurationVar(
		&opts.tlsTimeout,
		"tls-timeout",
		defaultTLSTimeout,
		"Maximum duration for each TLS attempt",
	)

	flags.StringVar(
		&opts.httpURL,
		"http-url",
		"",
		"Enable an independent HTTP check against this absolute URL",
	)

	flags.IntVar(
		&opts.httpExpectedStatus,
		"http-status",
		defaultHTTPStatus,
		"Expected HTTP response status code",
	)

	flags.DurationVar(
		&opts.httpTimeout,
		"http-timeout",
		defaultHTTPTimeout,
		"Maximum duration for the HTTP request",
	)

	flags.StringVar(
		&opts.format,
		"format",
		defaultOutputFormat,
		"Output format: text or json",
	)

	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: netprobe -target <hostname> [options]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		flags.PrintDefaults()
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Examples:")
		fmt.Fprintln(stderr, "  netprobe -target example.com")
		fmt.Fprintln(stderr, "  netprobe -target example.com -port 443 -tls")
		fmt.Fprintln(stderr, "  netprobe -target example.com -http-url https://example.com/health")
		fmt.Fprintln(stderr, "  netprobe -target example.com -format json")
	}

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	if flags.NArg() != 0 {
		err := fmt.Errorf(
			"unexpected positional arguments: %s",
			strings.Join(flags.Args(), " "),
		)
		fmt.Fprintf(stderr, "netprobe: %v\n", err)
		return options{}, err
	}

	if strings.TrimSpace(opts.target) == "" {
		err := errors.New("the -target flag is required")
		fmt.Fprintf(stderr, "netprobe: %v\n", err)
		return options{}, err
	}

	opts.target = strings.TrimSpace(opts.target)

	return opts, nil
}

func validateOptions(opts options) error {
	if opts.port < 1 || opts.port > 65535 {
		return fmt.Errorf(
			"port must be between 1 and 65535: %d",
			opts.port,
		)
	}

	if opts.dnsTimeout <= 0 {
		return errors.New("DNS timeout must be greater than zero")
	}

	if opts.tcpTimeout <= 0 {
		return errors.New("TCP timeout must be greater than zero")
	}

	if opts.enableTLS && opts.tlsTimeout <= 0 {
		return errors.New(
			"TLS timeout must be greater than zero when TLS is enabled",
		)
	}

	if opts.format != "text" && opts.format != "json" {
		return fmt.Errorf(
			"unsupported output format %q: use text or json",
			opts.format,
		)
	}

	if opts.httpURL == "" {
		return nil
	}

	parsedURL, err := url.Parse(opts.httpURL)
	if err != nil {
		return fmt.Errorf("invalid HTTP URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("HTTP URL scheme must be http or https")
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return errors.New("HTTP URL must include a hostname")
	}

	if parsedURL.User != nil {
		return errors.New("HTTP URL must not contain user information")
	}

	if opts.httpExpectedStatus < 100 || opts.httpExpectedStatus > 599 {
		return fmt.Errorf(
			"HTTP expected status must be between 100 and 599: %d",
			opts.httpExpectedStatus,
		)
	}

	if opts.httpTimeout <= 0 {
		return errors.New(
			"HTTP timeout must be greater than zero when HTTP is enabled",
		)
	}

	return nil
}

func writeReport(
	w io.Writer,
	report diagnostic.Report,
	format string,
) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("encode JSON report: %w", err)
		}

		return nil

	case "text":
		if err := writeTextReport(w, report); err != nil {
			return fmt.Errorf("write text report: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeTextReport(w io.Writer, report diagnostic.Report) error {
	if _, err := fmt.Fprintf(
		w,
		"NetProbe diagnostic report\nTarget: %s\nStatus: %s\nDuration: %s\n",
		report.Target,
		strings.ToUpper(string(report.Status)),
		report.Duration,
	); err != nil {
		return err
	}

	for _, result := range report.Results {
		if _, err := fmt.Fprintf(
			w,
			"\n[%s] %s\n",
			strings.ToUpper(string(result.Status)),
			result.Name,
		); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "  Required: %t\n", result.Required); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "  Message: %s\n", result.Message); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "  Duration: %s\n", result.Duration); err != nil {
			return err
		}

		if result.Error != "" {
			if _, err := fmt.Fprintf(w, "  Error: %s\n", result.Error); err != nil {
				return err
			}
		}

		if len(result.Details) > 0 {
			if _, err := fmt.Fprintln(w, "  Details:"); err != nil {
				return err
			}

			for key, value := range result.Details {
				if _, err := fmt.Fprintf(w, "    %s: %v\n", key, value); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
