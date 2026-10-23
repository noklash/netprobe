# Architecture

## Purpose

NetProbe is a one-shot network connectivity diagnostic tool written in Go. It investigates individual stages of outbound connectivity and preserves the evidence collected at each stage.

The implementation provides:

- DNS resolution.
- Address-specific TCP connectivity checks.
- Optional TLS handshake and certificate verification.
- An independent HTTP response check.
- Diagnostic result aggregation.
- A runner that coordinates the checks.
- A command-line interface.
- Human-readable and JSON report output.

The architecture separates individual network checks, diagnostic orchestration, result aggregation, and presentation. This keeps network behavior independently testable and prevents a failure at one stage from erasing useful evidence collected at another.

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
- Keep HTTP response diagnostics independent from the DNS/TCP/TLS sequence.

## Implemented Diagnostic Flow

The runner in `internal/runner/` follows this sequence:

1. Validate the diagnostic context and configuration.
2. Resolve the target hostname.
3. Preserve the DNS result.
4. If DNS produces no usable addresses, record why dependent TCP and optional TLS checks were skipped.
5. Attempt a bounded TCP connection to each resolved IP address independently.
6. If TLS is enabled, perform a TLS handshake and certificate verification after each successful TCP check.
7. Record skipped TLS results when the corresponding TCP check does not pass.
8. If HTTP is enabled, make one independent HTTP request against the configured URL.
9. Preserve the individual results and aggregate them into a diagnostic report.
10. Write the report through the CLI and return the appropriate process exit code.

DNS is performed once per run. TCP checks are performed for each usable address returned by DNS. TLS checks are optional and disabled unless enabled through configuration.

The HTTP check is enabled separately. It is not dependent on the DNS, TCP, or TLS check outcomes. The HTTP request uses its configured URL and does not reuse the addresses or connections from those checks.

A failure against one address does not automatically prevent attempts against remaining addresses. Cancellation of the parent context affects subsequent operations; the runner records skipped results for checks that it cannot attempt.

The runner returns configuration or internal execution errors as Go errors when it cannot produce a valid report. Ordinary network outcomes are represented in diagnostic results and retained in the report.

## Components

### Network Checks

Location: `internal/checks/`

This package contains the individual network operations. The checks use injected dependencies where appropriate so their behavior can be tested without relying on public DNS servers or public network services.

#### DNS Resolution

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

#### TCP Connectivity

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

#### TLS Negotiation and Certificate Verification

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

#### HTTP Response Check

Implementation: `internal/checks/http.go`

The HTTP check sends one GET request to an absolute HTTP or HTTPS URL and compares the returned status code with the configured expected status.

It:

- Validates the context, HTTP client, URL, expected status code, and timeout.
- Requires an HTTP or HTTPS URL with a hostname.
- Rejects URL user information.
- Uses a bounded request context.
- Clones the configured HTTP client to avoid mutating its redirect policy.
- Does not follow redirects.
- Records the response status, protocol, content type, content length, redirect status, and whether a `Location` header is present.
- Does not read or retain the response body.
- Classifies request failures and timeouts as diagnostic results.
- Sanitizes the reported URL by removing user information, query parameters, and fragments.

A status code different from the expected status produces a failed result. A request that does not complete within the available context produces an inconclusive result. Other request failures are represented as failed results.

The check records observations about the HTTP response. It does not inspect response-body content or verify application-specific behavior.

##### HTTP Independence

The runner performs the HTTP check once when enabled.

The HTTP check:

- Uses the configured HTTP URL directly.
- Does not reuse resolved addresses from the DNS check.
- Does not reuse connections from the TCP or TLS checks.
- Does not depend on the outcomes of the DNS/TCP/TLS sequence.
- Retains its result even if a TCP or TLS check fails.

This separation is intentional. The DNS/TCP/TLS checks observe the target hostname and port through their own operations. The HTTP check observes the endpoint specified by the URL.

Consequently, the results may differ without being contradictory. For example, the TCP check for the `-target` and `-port` combination can fail while an HTTP request to a different hostname or port succeeds.

A passing HTTP check establishes only that the configured endpoint returned the expected status code. It does not establish that the response body is correct, that every application dependency is available, or that the intended business operation succeeded.

### Diagnostic Model and Report Aggregation

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

The report model does not execute network operations. It validates and aggregates results produced by the checks and runner.

### Diagnostic Runner

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
- Whether HTTP is enabled.
- HTTP URL.
- Expected HTTP status code.
- HTTP timeout.
- An optional HTTP client.
- Resolver and dialer dependencies.

DNS and TCP timeouts must be greater than zero. The TLS timeout must be greater than zero when TLS is enabled. The HTTP URL, expected status, and timeout must be valid when HTTP is enabled.

The target, port, resolver, dialer, and context must also be valid.

The runner performs DNS resolution once. If DNS does not produce usable addresses, it records skipped TCP results and, when TLS is enabled, skipped TLS results. An enabled HTTP check is still attempted because it is independent of the DNS/TCP/TLS sequence.

For each usable address, the runner performs a TCP check. If TLS is enabled, it performs a TLS check only after that address's TCP result passes. Otherwise, it records why the TLS check was skipped.

The runner performs one HTTP request after the DNS/TCP/TLS sequence, unless the DNS path returns early because no usable addresses were produced. In that case, it still attempts the independent HTTP check when enabled.

The runner preserves results and aggregates them through the diagnostic report model.

### Command-Line Interface

Implementation: `cmd/netprobe/main.go` and `internal/cli/`

The CLI parses and validates arguments, constructs runner configuration, executes the diagnostic, writes the report, and returns a process exit code.

The CLI supports:

- A required target hostname.
- A configurable TCP port.
- Separate DNS, TCP, TLS, and HTTP timeouts.
- Optional TLS checks.
- An optional independent HTTP check.
- An expected HTTP status code.
- Human-readable output.
- JSON output.
- Predictable process exit codes.

The CLI uses text output by default. JSON output is selected with `-format json`.

Supplying `-http-url` enables HTTP diagnostics. The default expected HTTP status is `200`.

The CLI validates arguments before running network checks. Invalid configuration is therefore distinct from a diagnostic report that records an unsuccessful network operation.

### User-Facing Reporting

The CLI provides human-readable and JSON report output.

The text report includes:

- Target.
- Overall status.
- Total duration.
- Individual check status.
- Whether each result is required.
- Check messages.
- Check duration.
- Error information when present.
- Structured details when available.

The JSON report includes the target, start time, total duration, overall status, and individual results.

Durations are serialized as integer nanosecond values in fields named `duration_ns`.

The report preserves the individual results so users can inspect the evidence behind the aggregate status.

Presentation code formats runner results. It does not perform network operations or independently infer root causes.

## Address-Family Isolation

IPv4 and IPv6 results must remain distinct. Success with one address does not prove that another address is reachable.

DNS results retain IPv4 and IPv6 addresses separately. The runner performs TCP checks against individual addresses, preserving each check's outcome.

A failed connection to one address does not automatically prevent attempts against other addresses unless the parent context has been canceled or has expired.

When TLS connects to a resolved IP address, server-name handling and certificate verification use the original target hostname.

Certificate verification must not be disabled as a workaround for failed checks.

## Timeouts and Resource Management

DNS, TCP, TLS, and HTTP operations use configured timeouts and the parent execution context to bound their operations.

An operation's effective deadline must not extend beyond an earlier deadline imposed by the parent context.

Cancellation and timeout outcomes must be distinguished from ordinary failures when the available error information permits it.

An inconclusive result means the operation did not establish its outcome within the available execution context. It is not proof that the destination is unreachable.

Successful TCP and TLS connections are closed by their respective checks. The HTTP response body is closed after the response is received.

NetProbe does not maintain persistent connections or run as a background service.

Prompt cancellation depends on the underlying operation and injected dependencies respecting the supplied context. A custom resolver or dialer that ignores cancellation may not return promptly.

## Testing

Core behavior is covered by unit tests using controlled dependencies and local test infrastructure where appropriate. Routine automated tests should not depend on public Internet access.

Tests should cover:

- Successful DNS, TCP, TLS, and HTTP operations.
- Resolution, connection, handshake, certificate-verification, and HTTP request failures.
- HTTP status mismatches.
- Redirect handling.
- Invalid inputs and dependencies.
- Timeout and cancellation behavior.
- Parent-context deadlines.
- IPv4 and IPv6 address handling.
- Independent outcomes for multiple resolved addresses.
- Skipped dependent checks.
- TLS being disabled or enabled.
- HTTP being disabled or enabled.
- Result validation and report aggregation.
- Text and JSON serialization.
- CLI argument validation and exit codes.
- Resource cleanup.

Public network experiments should be documented separately from automated tests. Experimental observations must identify the tested environment and must not be presented as universal guarantees.

## Initial Non-Goals

The current scope excludes:

- A background daemon or continuous monitoring service.
- Response-body inspection or application-specific HTTP assertions.
- Packet capture.
- Dashboards or a metrics backend.
- Cloud and Kubernetes integrations.
- Automatic network configuration changes.
- Privileged commands invoked implicitly.
- Definitive root-cause claims unsupported by collected evidence.

These boundaries may change as the project develops, but new capabilities should be documented and tested before being described as implemented.