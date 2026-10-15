# NetProbe

NetProbe is a Linux network connectivity diagnostic tool written in Go.

It investigates outbound connectivity through independently observable stages:

- DNS resolution
- Address-specific TCP connection establishment
- TLS negotiation and certificate verification

NetProbe is designed to preserve evidence from individual checks instead of collapsing different network failures into one ambiguous status.

The project emphasizes explicit failure semantics, bounded network operations, deterministic testing, and documentation that distinguishes observations from conclusions.

## Project Status

NetProbe is in active development.

The diagnostic result model, report aggregation, DNS check, TCP check, TLS check, and diagnostic runner have been implemented.

The runner coordinates DNS resolution and address-specific TCP checks. When TLS is enabled, it performs TLS checks after successful TCP checks and preserves skipped results when dependent checks cannot run.

The core components are independently testable. Tests cover successful operations, failures, cancellation, configuration validation, and dependent-check behavior without requiring routine access to public network services.

**The command-line interface and complete user-facing application are not yet implemented.** HTTP response diagnostics are also future work.

## Current Capabilities

| Capability | Status | Description |
|---|---|---|
| Diagnostic result model | Implemented | Represents individual outcomes, durations, errors, requirements, and supporting details. |
| Report aggregation | Implemented | Aggregates individual check results into an overall diagnostic status. |
| DNS resolution | Implemented | Resolves hostnames, normalizes addresses, removes duplicates, and preserves IPv4 and IPv6 information. |
| TCP connectivity | Implemented | Tests connections to individual IP addresses and ports using bounded operations. |
| TLS diagnostics | Implemented | Tests TLS negotiation and certificate verification using the intended hostname. |
| Diagnostic runner | Implemented | Coordinates DNS, TCP, and optional TLS checks while preserving individual results. |
| HTTP response diagnostics | Planned | Will evaluate HTTP responses separately from transport and TLS outcomes. |
| Command-line interface | Planned | Will provide user-facing arguments, configuration, and execution. |
| Human-readable output | Planned | Will present diagnostic reports in a readable format. |
| JSON output | Planned | Will expose structured reports for automation and integration. |
| Documented exit codes | Planned | Will provide predictable process exit behavior for automation. |

Implemented components do not yet constitute a complete end-to-end command-line application.

## Design Principles

### Evidence First

A diagnostic result records what an operation observed. A failed connection does not automatically establish its root cause.

NetProbe avoids claiming that a remote service, network route, firewall, or local system is responsible unless the available evidence supports that conclusion.

### Independent Checks

DNS, TCP, and TLS represent different operations and must retain their individual outcomes.

A successful DNS lookup does not prove that a destination is reachable. A successful TCP connection does not prove that TLS negotiation will succeed.

Failures in dependent checks should not erase useful results from earlier checks.

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
- **Failed:** The check produced evidence of an unsuccessful outcome.
- **Skipped:** The check could not or should not be attempted under the applicable conditions.
- **Inconclusive:** The available evidence does not establish a definitive outcome.

Overall status is derived from individual results and their required or optional status.

### Secure TLS Verification

TLS checks retain normal certificate verification and use the intended hostname for certificate verification and server-name handling, even when the connection targets a resolved IP address.

The implementation does not disable certificate verification as a general workaround for failed connections.

### Testability

Core behavior is tested using controlled dependencies where appropriate. DNS resolvers and network dialers can be substituted to test success, failure, cancellation, and other edge cases without relying on the public Internet.

### Focused Scope

NetProbe investigates network connectivity rather than general host health.

It complements Hostcheck, which examines Linux host resources and health. NetProbe focuses on whether specific network operations succeed from the environment where it runs.

## How the Diagnostic Runner Works

The runner follows this sequence:

1. Validate the diagnostic configuration.
2. Resolve the target hostname.
3. Preserve the DNS result.
4. If DNS produces no usable addresses, record why dependent TCP checks were skipped.
5. Attempt a bounded TCP connection to each resolved address independently.
6. If TLS is enabled and a TCP check succeeds, perform a TLS handshake and verify the certificate.
7. Preserve individual check results and aggregate them into a diagnostic report.

A failure against one address does not automatically prevent attempts against other addresses. Cancellation or expiration of the parent context affects subsequent checks, and skipped results explain why those checks were not attempted.

TLS is optional because not every TCP service speaks TLS.

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
├── internal/
│   ├── checks/
│   │   ├── dns.go
│   │   ├── dns_test.go
│   │   ├── tcp.go
│   │   ├── tcp_test.go
│   │   ├── tls.go
│   │   └── tls_test.go
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
go test ./...
```

Run static analysis:

```bash
go vet ./...
```

Run the runner tests with verbose output:

```bash
go test ./internal/runner -v
```

Format Go source files with:

```bash
gofmt -w internal/checks/*.go internal/diagnostic/*.go internal/runner/*.go
```

These commands verify different aspects of the project. Passing unit tests and static analysis does not, by itself, establish correct behavior under every real-world network condition.

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

NetProbe is being developed as an engineering project focused on understanding network behavior, building testable infrastructure diagnostics, and documenting the reasoning behind implementation decisions.

Future development will prioritize a usable command-line interface, structured diagnostic output, HTTP response checks, predictable exit behavior, and reliable end-to-end execution.

The objective is not simply to report whether a connection works. It is to provide useful, bounded, and defensible evidence about which stages of a network operation succeeded or failed.