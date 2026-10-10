# Architecture

## Purpose

NetProbe is a one-shot command-line tool for diagnosing outbound network connectivity. It examines individual connection stages and preserves the evidence collected at each stage.

## Design Goals

- Separate diagnostic orchestration from individual network checks.
- Preserve successful results when other checks fail.
- Evaluate IPv4 and IPv6 independently.
- Bound network operations with explicit timeouts.
- Record errors and operation durations.
- Support human-readable and JSON reports.
- Test core behavior without depending on public Internet access.
- Minimize external dependencies.

## Diagnostic Flow

The planned flow is:

1. Validate the command-line input.
2. Identify the target hostname, scheme, and port.
3. Resolve the hostname and retain returned addresses.
4. Classify addresses by family.
5. Attempt bounded TCP connections to applicable addresses.
6. Perform TLS negotiation for HTTPS targets.
7. Request and evaluate an HTTP response when applicable.
8. Aggregate individual results without discarding their evidence.
9. Render the report.

If a prerequisite fails, dependent checks must be marked as skipped or unavailable with an explanation. They must not be reported as successful.

## Components

### Command-line interface

Location: `cmd/netprobe/`

Responsible for argument parsing, validation, configuration, invoking the diagnostic runner, rendering output, and returning a documented exit status.

### Diagnostic model and runner

Location: `internal/diagnostic/`

Defines result types, coordinates checks, preserves partial results, and derives the overall summary.

The runner should be testable without requiring live public endpoints.

### Network checks

Location: `internal/checks/`

Contains DNS, TCP, TLS, and HTTP checks.

The initial implementation should prefer Go's standard library over shelling out to system commands.

Linux-specific route inspection may be considered later as a separate capability.

### Reporting

Location: `internal/report/`

Renders diagnostic results as human-readable text or JSON. Reporting code must not perform network operations or independently infer root causes.

## Address-Family Isolation

IPv4 and IPv6 results must remain distinct. Success with one family must not hide failure with the other.

When connecting to a selected IP address, TLS Server Name Indication (SNI) and certificate verification must still use the intended hostname. Certificate verification must not be disabled to force a successful result.

## Timeouts and Resources

Every network operation must have an explicit time limit. Connections and response bodies must be closed appropriately.

Timeouts should be distinguishable from other errors when the available error information permits it.

## Testing

Use deterministic unit tests and controlled local servers where practical. Tests should cover successful operations, failures, timeouts, partial success, skipped checks, and report serialization.

Public Internet experiments should be documented separately from automated tests.

## Initial Non-Goals

The first milestone excludes:

- A background daemon or continuous monitoring.
- Packet capture.
- Dashboards or a metrics backend.
- Cloud and Kubernetes integrations.
- Automatic network configuration changes.
- Definitive root-cause claims unsupported by the collected evidence.
