# Architecture

## Purpose

NetProbe is a one-shot network connectivity diagnostic tool written in Go. It investigates individual stages of outbound connectivity and preserves the evidence collected at each stage.

The current implementation provides DNS resolution, address-specific TCP connectivity checks, optional TLS handshake and certificate verification, diagnostic result aggregation, and a runner that coordinates these operations.

The command-line interface, HTTP response diagnostics, and user-facing report rendering are not yet implemented.

The architecture separates individual network checks, diagnostic orchestration, result aggregation, and eventual presentation. This keeps network behavior independently testable and prevents a failure at one stage from erasing useful evidence collected at another.

## Design Goals

- Separate network checks from diagnostic orchestration.
- Preserve individual outcomes and partial success.
- Evaluate resolved IP addresses independently.
- Keep IPv4 and IPv6 results distinct.
- Bound network operations with explicit timeouts and context cancellation.
- Distinguish operational network outcomes from invalid configuration and internal errors.
- Represent skipped and inconclusive checks explicitly.
- Retain structured evidence and operation durations.
- Support deterministic tests without routine dependence on public Internet services.
- Avoid conclusions that exceed the available evidence.
- Minimize external dependencies.

## Implemented Diagnostic Flow

The runner in `internal/runner/` currently follows this sequence:

1. Validate the diagnostic context and configuration.
2. Resolve the target hostname.
3. Preserve the DNS result.
4. If DNS produces no usable addresses, record why dependent checks were skipped.
5. Attempt a bounded TCP connection to each resolved IP address independently.
6. If TLS is enabled, perform a TLS handshake and certificate verification after each successful TCP check.
7. Record skipped TLS results when the corresponding TCP check does not pass.
8. Preserve the individual results and aggregate them into a diagnostic report.

DNS is performed once per run. TCP checks are performed for each usable address returned by DNS. TLS checks are optional and disabled unless enabled through runner configuration.

A failure against one address does not automatically prevent attempts against remaining addresses. Cancellation of the parent context affects subsequent operations; the runner records skipped results for checks that it cannot attempt.

The runner returns configuration or internal execution errors as Go errors. Ordinary network outcomes are represented in diagnostic results and retained in the report.

## Components

### Network checks

Location: `internal/checks/`

This package contains the individual network operations. The checks use injected dependencies so that their behavior can be tested without relying on public DNS servers or public network services.

#### DNS resolution

Implementation: `internal/checks/dns.go`

The DNS check resolves the target hostname through a resolver dependency.

It:

- Validates its context, resolver, and hostname.
- Records the hostname and operation duration.
- Filters invalid addresses.
- Normalizes IPv4-mapped IPv6 addresses.
- Removes duplicate addresses.
- Sorts addresses for deterministic results.
- Retains IPv4 and IPv6 addresses separately.
- Records the number of usable addresses.
- Represents resolution failures as diagnostic results.
- Classifies context cancellation and deadline expiration as inconclusive.

A successful DNS result establishes that the resolver returned usable IP addresses. It does not establish that those addresses accept connections.

#### TCP connectivity

Implementation: `internal/checks/tcp.go`

The TCP check attempts to establish a connection to a literal IP address and port through an injected dialer. It does not perform DNS resolution.

It:

- Validates the context, dialer, IP address, port, and timeout.
- Uses a bounded connection attempt.
- Respects an earlier deadline imposed by the parent context.
- Formats IPv4 and IPv6 destinations correctly.
- Records the destination, port, configured timeout, and duration.
- Represents ordinary connection failures as failed results.
- Classifies cancellation and deadline expiration as inconclusive.
- Closes a successful connection after measuring connection establishment.

A successful TCP check establishes that a connection was opened to the selected IP address and port. It does not prove that the application protocol is healthy or that TLS or HTTP will succeed.

The check depends on the supplied dialer respecting context cancellation. A custom dialer that ignores its context may not return promptly.

#### TLS negotiation and certificate verification

Implementation: `internal/checks/tls.go`

The TLS check connects to a literal IP address and port, then performs a TLS handshake.

It:

- Validates its context, dialer, address, hostname, port, and timeout.
- Uses a bounded operation that respects an earlier parent-context deadline.
- Uses the original target hostname for TLS server-name handling and certificate verification.
- Requires normal certificate verification.
- Uses Go's system certificate pool when no custom root pool is supplied.
- Requires at least TLS 1.2.
- Records the target address, hostname, port, timeout, and outcome.
- Includes negotiated TLS version, cipher suite, and verified-chain count on success.
- Classifies ordinary handshake or verification failures as failed results.
- Classifies cancellation and deadline expiration as inconclusive.
- Closes the TLS connection after the check.

The runner invokes TLS only when enabled and only after the TCP check for the corresponding address passes.

The TLS check opens its own connection. Therefore, the preceding TCP result and the TLS result are separate observations rather than measurements of one reused connection.

A successful TLS check establishes that the handshake and certificate verification succeeded under the configured trust settings. It does not establish that an HTTP endpoint or application function is healthy.

### Diagnostic model and report aggregation

Location: `internal/diagnostic/`

This package defines the structures used to represent and aggregate diagnostic evidence.

Implemented capabilities include:

- Individual results with a name, status, required-check flag, duration, message, optional error, and structured details.
- Validation of individual results.
- JSON serialization with durations expressed in nanoseconds.
- A report containing the target, start time, total duration, overall status, and individual results.
- Validation of reports and their individual results.
- Derivation of overall status from required results.
- Mapping of report status to a process exit-code value.

The overall status is derived using these rules:

1. A failed required check makes the overall report failed.
2. Otherwise, a required check that is inconclusive or skipped makes the report inconclusive.
3. If no required checks exist, the report is inconclusive.
4. Otherwise, all required checks passed and the report is passed.

Optional results remain visible but do not determine the overall status when all required checks pass.

The exit-code mapping is implemented as a method on the report model. The command-line interface that would use the mapping and terminate the process has not been implemented.

### Diagnostic runner

Location: `internal/runner/`

The runner validates configuration, executes checks in dependency order, preserves individual outcomes, and builds the final report.

Its configuration includes:

- Target hostname.
- Destination port.
- DNS timeout.
- TCP timeout.
- TLS timeout.
- Whether TLS checks are enabled.
- An optional TLS certificate root pool.
- Resolver and dialer dependencies.

DNS and TCP timeouts must be greater than zero. The TLS timeout must be greater than zero when TLS is enabled. The target, port, resolver, dialer, and context must also be valid.

The runner performs DNS resolution once. If DNS does not produce usable addresses, it records skipped TCP results and, when TLS is enabled, skipped TLS results.

For each usable address, the runner performs a TCP check. If TLS is enabled, it performs a TLS check only after that address's TCP result passes. Otherwise, it records why the TLS check was skipped.

The runner preserves results across addresses and aggregates them through the diagnostic report model. It does not attempt HTTP requests.

### Command-line interface

Planned location: `cmd/netprobe/`

The CLI is not yet implemented.

Its intended responsibilities are:

- Parse command-line arguments.
- Validate user input and configuration.
- Construct the runner configuration.
- Invoke the diagnostic runner.
- Present the diagnostic report.
- Return a documented process exit status.
- Distinguish invalid invocation or internal errors from ordinary network outcomes.

### User-facing reporting

A dedicated human-readable reporting layer is not yet implemented.

Individual results and reports support JSON serialization through the diagnostic model. This does not yet provide a complete user-facing JSON command or a command-line application.

Future presentation code should format the results produced by the runner. It must not perform network operations or independently infer root causes.

## Address-Family Isolation

IPv4 and IPv6 results must remain distinct. Success with one address does not prove that another address is reachable.

DNS results retain IPv4 and IPv6 addresses separately. The runner performs TCP checks against individual addresses, preserving each check's outcome.

A failed connection to one address does not automatically prevent attempts against other addresses unless the parent context has been canceled or has expired.

When TLS connects to a resolved IP address, server-name handling and certificate verification use the original target hostname. Certificate verification must not be disabled as a workaround for failed checks.

## Timeouts and Resource Management

DNS, TCP, and TLS operations are bounded by configured timeouts and the parent execution context.

An operation's effective deadline must not extend beyond an earlier deadline imposed by the parent context.

Cancellation and timeout outcomes must be distinguished from ordinary failures when the available error information permits it. An inconclusive result means the operation did not establish its outcome within the available execution context; it is not proof that the destination is unreachable.

Successful TCP and TLS connections are closed by their respective checks. NetProbe does not maintain persistent connections or run as a background service.

## Testing

Core behavior is covered by unit tests using controlled dependencies and local test infrastructure where appropriate. Routine automated tests should not depend on public Internet access.

Tests should cover:

- Successful DNS, TCP, and TLS operations.
- Resolution, connection, handshake, and certificate-verification failures.
- Invalid inputs and dependencies.
- Timeout and cancellation behavior.
- Parent-context deadlines.
- IPv4 and IPv6 address handling.
- Independent outcomes for multiple resolved addresses.
- Skipped dependent checks.
- TLS being disabled or enabled.
- Result validation and report aggregation.
- JSON serialization and resource cleanup.

Public network experiments should be documented separately from automated tests. Experimental observations must identify the tested environment and must not be presented as universal guarantees.

## Initial Non-Goals

The current scope excludes:

- A background daemon or continuous monitoring service.
- HTTP response diagnostics.
- Packet capture.
- Dashboards or a metrics backend.
- Cloud and Kubernetes integrations.
- Automatic network configuration changes.
- Privileged commands invoked implicitly.
- Definitive root-cause claims unsupported by collected evidence.

These boundaries may change as the project develops, but new capabilities should be documented and tested before being described as implemented.
