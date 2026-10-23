# NetProbe

NetProbe is a Linux network connectivity diagnostic tool written in Go.

It investigates outbound connectivity through independently observable operations:

- DNS resolution
- Address-specific TCP connection establishment
- Optional TLS negotiation and certificate verification
- Optional HTTP response diagnostics

NetProbe preserves evidence from individual checks instead of collapsing different network outcomes into one ambiguous status.

The project emphasizes explicit failure semantics, bounded network operations, deterministic testing, and documentation that distinguishes observations from conclusions.

## Project Status

NetProbe is in active development.

The current implementation includes:

- A diagnostic result and report model.
- DNS resolution with IPv4 and IPv6 address handling.
- TCP connectivity checks against individual resolved addresses.
- Optional TLS handshake and certificate verification.
- An independent HTTP GET diagnostic.
- Diagnostic orchestration and report aggregation.
- A command-line interface.
- Human-readable and JSON report output.
- Documented process exit codes.

The command-line interface supports configurable operation timeouts, optional TLS checks, and an optional HTTP check with a configurable expected response status.

The HTTP check is independent of the DNS, TCP, and TLS check sequence. It uses the configured HTTP URL directly and does not reuse connections or resolved addresses from those checks.

The core components are independently testable. Tests cover successful operations, failures, cancellation, configuration validation, report generation, and dependent-check behavior without requiring routine access to public network services.

NetProbe is a one-shot diagnostic tool. It is not a continuous monitoring platform, a general application-health system, or a definitive root-cause analysis engine.

## Current Capabilities

| Capability | Status | Description |
|---|---|---|
| Diagnostic result model | Implemented | Represents individual outcomes, durations, errors, required-check flags, and supporting details. |
| Report aggregation | Implemented | Derives the overall diagnostic status from individual required-check results. |
| DNS resolution | Implemented | Resolves hostnames, normalizes addresses, removes duplicates, and preserves IPv4 and IPv6 information. |
| TCP connectivity | Implemented | Tests connections to individual IP addresses and ports using bounded operations. |
| TLS diagnostics | Implemented | Tests TLS negotiation and certificate verification using the intended hostname. |
| HTTP response diagnostics | Implemented | Sends an independent HTTP GET request and compares the response status with the expected status code. |
| Diagnostic runner | Implemented | Coordinates DNS, TCP, optional TLS, and optional independent HTTP checks while preserving individual results. |
| Command-line interface | Implemented | Parses arguments, validates configuration, executes diagnostics, and returns process exit codes. |
| Human-readable output | Implemented | Presents the report status and individual check results in readable text. |
| JSON output | Implemented | Exposes structured diagnostic reports for inspection and automation. |
| Process exit codes | Implemented | Distinguishes successful diagnostics, diagnostic failures, invalid arguments, and internal errors. |

## Installation and Execution

NetProbe requires Go.

From the repository root, run the tool directly:

```bash
go run ./cmd/netprobe -target example.com
```

To build a binary:

```bash
go build -o netprobe ./cmd/netprobe
```

Run the resulting binary:

```bash
./netprobe -target example.com
```

The `-target` argument is required.

## Usage

### DNS and TCP diagnostics

Run the default DNS and TCP checks:

```bash
./netprobe -target example.com
```

The default TCP port is `443`.

To select another port:

```bash
./netprobe -target example.com -port 80
```

DNS resolution is performed once. If usable addresses are returned, NetProbe attempts TCP connectivity to each resolved address independently.

### TLS diagnostics

Enable TLS handshake and certificate verification:

```bash
./netprobe -target example.com -port 443 -tls
```

TLS is disabled by default because not every TCP service speaks TLS.

When TLS is enabled, NetProbe attempts TLS only after the corresponding TCP check succeeds. TLS establishes its own connection, so the TCP and TLS results represent separate observations.

Certificate verification uses the original target hostname rather than the resolved IP address.

### HTTP response diagnostics

Enable the independent HTTP diagnostic:

```bash
./netprobe \
  -target example.com \
  -http-url https://example.com/health \
  -http-status 200
```

Supplying `-http-url` enables the HTTP check.

The HTTP check:

- Sends a GET request to the configured absolute HTTP or HTTPS URL.
- Compares the returned status code with the expected status.
- Defaults to an expected status code of `200`.
- Does not follow redirects.
- Does not read or retain the response body.
- Uses its own timeout.
- Does not reuse the DNS/TCP/TLS checks' connections or resolved addresses.

A matching status code establishes that the endpoint returned the expected code. It does not establish that the response body is correct or that the application and its dependencies are healthy.

The hostname supplied through `-target` is used for DNS, TCP, and optional TLS diagnostics. The HTTP request uses the hostname in `-http-url`.

### JSON output

To produce a structured JSON report:

```bash
./netprobe -target example.com -format json
```

JSON output contains the target, start time, overall status, and individual results.

Durations are represented as integer nanosecond values in fields named `duration_ns`.

### Timeouts

The CLI exposes separate timeout settings for the different operations.

| Flag | Default | Purpose |
|---|---:|---|
| `-dns-timeout` | `2s` | Maximum duration for DNS resolution. |
| `-tcp-timeout` | `2s` | Maximum duration for each TCP connection attempt. |
| `-tls-timeout` | `3s` | Maximum duration for each TLS attempt. |
| `-http-timeout` | `3s` | Maximum duration for the HTTP request. |

The TLS timeout applies when TLS is enabled. The HTTP timeout applies when an HTTP URL is supplied.

All configured timeouts must be greater than zero when the corresponding operation is enabled.

An earlier deadline imposed by the parent execution context takes precedence over an operation's configured timeout.

A timeout does not necessarily establish that a destination is permanently unreachable.

### Command-line options

| Flag | Required | Default | Description |
|---|---|---|---|
| `-target` | Yes | None | Hostname to diagnose using DNS and TCP, and optionally TLS. |
| `-port` | No | `443` | TCP port to check. |
| `-dns-timeout` | No | `2s` | DNS resolution timeout. |
| `-tcp-timeout` | No | `2s` | Timeout for each TCP connection attempt. |
| `-tls` | No | `false` | Enable TLS handshake and certificate verification. |
| `-tls-timeout` | No | `3s` | Timeout for each TLS attempt. |
| `-http-url` | No | Empty | Enable an independent HTTP check against this absolute URL. |
| `-http-status` | No | `200` | Expected HTTP response status code. |
| `-http-timeout` | No | `3s` | HTTP request timeout. |
| `-format` | No | `text` | Output format: `text` or `json`. |

The HTTP URL must use the `http` or `https` scheme, include a hostname, and must not contain URL user information.

The expected HTTP status code must be between `100` and `599`.

### Exit codes

NetProbe uses process exit codes to distinguish diagnostic outcomes from invocation and internal errors.

| Exit code | Meaning |
|---:|---|
| `0` | All required diagnostic checks passed, or help was requested. |
| `1` | Required diagnostic checks did not all pass. |
| `2` | Invalid command-line arguments or configuration. |
| `3` | An internal error prevented a valid report from being produced or written. |

An exit code of `1` can represent a failed or inconclusive report. A skipped required check also prevents the tool from claiming that all required checks passed.

A diagnostic failure is different from an invalid invocation. For example, an unreachable TCP destination is a diagnostic outcome, while an invalid port is a configuration error.

The process exit code is useful for shell scripts and automation, but the report should still be inspected to understand the individual check outcomes.

## Design Principles

### Evidence First

A diagnostic result records what an operation observed.

A failed connection does not automatically establish its root cause.

NetProbe avoids claiming that a remote service, network route, firewall, resolver, or local system is responsible unless the available evidence supports that conclusion.

### Independent Checks

DNS, TCP, TLS, and HTTP represent distinct operations.

A successful DNS lookup does not prove that a destination is reachable. A successful TCP connection does not prove that TLS negotiation will succeed. A successful TLS handshake does not prove that an HTTP endpoint will return the expected status.

HTTP is evaluated independently from the DNS/TCP/TLS check sequence.

Failures in one check should not erase useful results from other checks.

### Explicit Address Families

IPv4 and IPv6 destinations are examined independently.

A failure involving one destination does not establish that every destination, or the entire address family, is unreachable.

### Bounded Execution

Network operations use explicit timeouts and support context cancellation where applicable.

The effective operation deadline must respect an earlier deadline imposed by the parent context.

A timeout or cancellation is not automatically proof that a destination is unreachable.

### Explicit Failure Semantics

NetProbe distinguishes between:

- **Passed:** The check completed successfully.
- **Failed:** The check produced evidence that its success condition was not satisfied.
- **Skipped:** The check could not or should not be attempted under the applicable conditions.
- **Inconclusive:** The available evidence does not establish a definitive outcome.

Overall status is derived from individual results and their required or optional status.

A failed required check takes precedence over skipped or inconclusive required checks. If no required check fails but at least one required check is skipped or inconclusive, the overall status is inconclusive.

### Secure TLS Verification

TLS checks retain normal certificate verification and use the intended hostname for certificate verification and server-name handling, even when the connection targets a resolved IP address.

The implementation does not disable certificate verification as a general workaround for failed connections.

### Testability

Core behavior is tested using controlled dependencies where appropriate.

DNS resolvers and network dialers can be substituted to test success, failure, cancellation, and other edge cases without relying on the public Internet.

### Focused Scope

NetProbe investigates network connectivity rather than general host health.

It complements Hostcheck, which examines Linux host resources and health. NetProbe focuses on whether specific network operations succeed from the environment where it runs.

## How the Diagnostic Runner Works

The runner follows this sequence:

1. Validate the diagnostic context and configuration.
2. Resolve the target hostname.
3. Preserve the DNS result.
4. If DNS produces no usable addresses, record why dependent TCP and optional TLS checks were skipped.
5. Attempt a bounded TCP connection to each resolved address independently.
6. If TLS is enabled and a TCP check succeeds, perform a TLS handshake and verify the certificate.
7. If HTTP is enabled, perform one independent HTTP request against the configured URL.
8. Preserve the individual check results and aggregate them into a diagnostic report.

A failure against one address does not automatically prevent attempts against other addresses. Cancellation or expiration of the parent context affects subsequent checks, and skipped results explain why those checks were not attempted.

TLS is optional because not every TCP service speaks TLS.

HTTP is independent of the DNS/TCP/TLS sequence. It uses the supplied URL directly, and its result is preserved even when a TCP or TLS check fails.

### TLS Configuration

The runner supports the following TLS settings:

| Setting | Purpose |
|---|---|
| `EnableTLS` | Enables TLS checks. TLS is disabled by default. |
| `TLSTimeout` | Sets the timeout for TLS operations when TLS is enabled. |
| `TLSRoots` | Optionally supplies a trusted certificate pool. When nil, Go uses the system certificate pool. |

When TLS is enabled, its timeout must be greater than zero.

TLS checks run only after successful TCP checks for the corresponding addresses. The TLS check establishes its own connection, so TCP and TLS are separate observations rather than measurements of a single reused connection.

Certificate verification uses the original target hostname rather than the resolved IP address.

## Repository Structure

```text
netprobe/
├── cmd/
│   └── netprobe/
│       └── main.go
├── internal/
│   ├── checks/
│   │   ├── dns.go
│   │   ├── dns_test.go
│   │   ├── tcp.go
│   │   ├── tcp_test.go
│   │   ├── tls.go
│   │   ├── tls_test.go
│   │   ├── http.go
│   │   └── http_test.go
│   ├── cli/
│   │   ├── cli.go
│   │   └── cli_test.go
│   ├── diagnostic/
│   │   ├── report.go
│   │   ├── report_test.go
│   │   ├── result.go
│   │   └── result_test.go
│   └── runner/
│       ├── runner.go
│       └── runner_test.go
├── docs/
│   ├── architecture.md
│   ├── diagnostic-model.md
│   ├── limitations.md
│   └── experiments/
│       └── 001-ipv6-connectivity-baseline.md
├── CHANGELOG.md
├── go.mod
└── README.md
```

The `internal` packages contain the diagnostic implementation and are not intended as public Go packages for external consumers.

## Development and Verification

NetProbe uses Go and its standard testing tools.

Run the full test suite from the repository root:

```bash
go test -count=1 ./...
```

Run the race-enabled test suite:

```bash
go test -race ./...
```

Run static analysis:

```bash
go vet ./...
```

Build all packages:

```bash
go build ./...
```

Run the runner tests with verbose output:

```bash
go test ./internal/runner -v
```

Check the patch for whitespace errors:

```bash
git diff --check
```

These commands verify different aspects of the project. Passing unit tests and static analysis does not, by itself, establish correct behavior under every real-world network condition.

Real-network smoke tests should supplement unit tests when needed. Their results depend on the environment and should not replace deterministic tests.

## Experiments and Limitations

NetProbe includes documented experiments and limitations.

The IPv6 connectivity baseline records observations from a particular environment. It should not be treated as proof of a universal networking or VirtualBox behavior.

Diagnostic results depend on the observation time, configured timeouts, resolver behavior, destination addresses, local network configuration, and the environment in which NetProbe runs.

For a more detailed discussion, see:

- [Architecture](docs/architecture.md)
- [Diagnostic Model](docs/diagnostic-model.md)
- [Limitations](docs/limitations.md)
- [IPv6 Connectivity Baseline](docs/experiments/001-ipv6-connectivity-baseline.md)
- [Changelog](CHANGELOG.md)

## Project Direction

NetProbe is an engineering project focused on understanding network behavior, building testable infrastructure diagnostics, and documenting the reasoning behind implementation decisions.

Future work should prioritize reliable end-to-end execution, integration coverage, clearer diagnostic evidence, and further refinement of tests and documentation.

The objective is not simply to report whether a connection works. It is to provide useful, bounded, and defensible evidence about which network operations succeeded or failed, while remaining explicit about what the observations cannot establish.