# Architecture

## Purpose

NetProbe is a one-shot command-line tool for diagnosing outbound network connectivity. It examines individual connection stages and preserves the evidence collected at each stage.

The design separates individual diagnostic checks from orchestration, result aggregation, and report rendering. This allows each check to be tested independently and makes it possible to retain useful evidence when one stage fails.

## Design Goals

- Separate diagnostic orchestration from individual network checks.
- Preserve successful results when other checks fail.
- Evaluate IPv4 and IPv6 independently.
- Bound network operations with explicit timeouts and context cancellation.
- Record errors, operation durations, and relevant diagnostic details.
- Support human-readable and JSON reports.
- Test core behavior without depending on public Internet access.
- Minimize external dependencies.
- Distinguish observed failures from inconclusive results.
- Avoid conclusions that exceed the evidence collected.

## Diagnostic Flow

The planned diagnostic flow is:

1. Validate the command-line input.
2. Identify the target hostname, scheme, and port.
3. Resolve the hostname and retain the returned addresses.
4. Classify addresses by family.
5. Attempt bounded TCP connections to applicable addresses.
6. Perform TLS negotiation for HTTPS targets.
7. Request and evaluate an HTTP response when applicable.
8. Aggregate individual results without discarding their evidence.
9. Render the report.

DNS resolution and TCP connectivity are implemented as independent checks. The complete command-line workflow and orchestration of these checks remain future work.

Each stage should produce a result that describes its own outcome. A prerequisite failure must not be converted into a successful result for a dependent check. Where a prerequisite is unavailable, dependent checks should be marked as skipped or otherwise unavailable with an explanation.

A failed check must not erase evidence collected by earlier successful checks. The final report should preserve individual outcomes so that partial success remains visible.

## Components

### Command-line interface

Location: `cmd/netprobe/`

Planned responsibility: argument parsing, input validation, configuration, invoking the diagnostic runner, rendering output, and returning a documented exit status.

The command-line interface is not yet implemented.

### Diagnostic model and runner

Location: `internal/diagnostic/`

The diagnostic model defines individual check results and aggregates them into a report.

Implemented capabilities include:

- A result model with status, required-check metadata, duration, message, error, and structured details.
- Validation of diagnostic results.
- JSON serialization of individual results.
- Report aggregation that preserves individual results and derives an overall summary.

The runner responsible for executing checks in the required order, managing shared cancellation and deadlines, and retaining partial results remains future work.

### Network checks

Location: `internal/checks/`

This package contains individual network diagnostic checks.

#### DNS resolution

The DNS check resolves a hostname through an injected resolver dependency.

It records the hostname, resolution duration, returned IPv4 and IPv6 addresses, and the number of usable addresses. It removes invalid and duplicate addresses and sorts the resulting addresses for deterministic output.

A resolution failure is represented as a diagnostic result. Context cancellation and deadline expiration are classified as inconclusive because the operation did not complete within the available context.

#### TCP connectivity

The TCP check attempts to establish a connection to a specific IP address and port through an injected dialer.

It does not resolve hostnames. Keeping resolution separate allows the diagnostic workflow to distinguish a DNS failure from a TCP connection failure.

The check:

- Validates its context, dialer, IP address, port, and timeout.
- Uses an explicit timeout while respecting an earlier parent-context deadline.
- Formats IPv4 and IPv6 targets correctly.
- Records the target address, port, configured timeout, and operation duration.
- Classifies connection failures as failed results.
- Classifies context cancellation and deadline expiration as inconclusive results.
- Closes successful connections before returning.

A successful TCP check establishes that a TCP connection was opened. It does not establish that the application protocol is healthy, that TLS negotiation will succeed, or that an HTTP request will receive a successful response.

The check relies on the supplied dialer respecting context cancellation. A dialer that ignores its context may prevent the operation from returning promptly.

#### Planned checks

TLS negotiation and HTTP response evaluation are planned capabilities. They have not yet been implemented.

The initial implementation should prefer Go's standard library over shelling out to system commands.

Linux-specific route inspection may be considered later as a separate capability.

### Reporting

Location: `internal/report/`

Planned responsibility: rendering diagnostic results as human-readable text or JSON.

Reporting code must not perform network operations or independently infer root causes. It should present the evidence and summary produced by the diagnostic model and runner.

The reporting package is not yet implemented.

## Address-Family Isolation

IPv4 and IPv6 results must remain distinct. Success with one family must not hide failure with the other.

DNS results retain IPv4 and IPv6 addresses separately. TCP checks operate on individual IP addresses, allowing each address to be tested independently.

When a diagnostic workflow evaluates multiple addresses, it should preserve the outcome for each attempted address rather than treating one successful connection as proof that every address is reachable.

When connecting to a selected IP address, TLS Server Name Indication (SNI) and certificate verification must still use the intended hostname. Certificate verification must not be disabled to force a successful result.

## Timeouts and Resources

Every network operation must have an explicit time limit. Connections and response bodies must be closed appropriately.

The DNS and TCP checks accept contexts so that callers can control cancellation and deadlines. The TCP check additionally accepts a connection timeout. Its effective deadline must not extend beyond an earlier deadline imposed by its parent context.

Timeouts and cancellation should be distinguishable from other errors when the available error information permits it.

An inconclusive result indicates that the check could not establish its outcome within the available execution context. It must not be treated as proof that the destination is unreachable.

Resource ownership must remain explicit. A successful TCP connection is closed by the TCP check because the check only measures connection establishment and does not maintain a session.

## Testing

Use deterministic unit tests and controlled local servers where practical. Automated tests must not depend on public Internet access.

Tests should cover:

- Successful operations.
- Connection and resolution failures.
- Invalid inputs and dependencies.
- Timeouts and context cancellation.
- Parent-context deadlines.
- IPv4 and IPv6 behavior.
- Partial success and independent address outcomes.
- Skipped or unavailable dependent checks.
- Result validation and report serialization.
- Appropriate resource cleanup.

The DNS and TCP checks use injected dependencies to test behavior without requiring live DNS servers or public network services.

Public Internet experiments should be documented separately from automated tests. Experimental observations must include enough context to explain what was tested and must not be presented as universal guarantees.

## Initial Non-Goals

The first milestone excludes:

- A background daemon or continuous monitoring.
- Packet capture.
- Dashboards or a metrics backend.
- Cloud and Kubernetes integrations.
- Automatic network configuration changes.
- Definitive root-cause claims unsupported by the collected evidence.