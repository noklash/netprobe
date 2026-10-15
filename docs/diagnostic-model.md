# Diagnostic Model

## Purpose

NetProbe evaluates network connectivity through distinct diagnostic operations. Each check records an observation about a specific operation, such as DNS resolution, a TCP connection attempt, or a TLS handshake and certificate verification.

The diagnostic model preserves individual observations and derives an overall status from the checks designated as required.

The implementation separates three concerns:

1. Executing network checks and collecting evidence.
2. Aggregating check results into an overall diagnostic status.
3. Mapping the overall status to a process exit-code value.

These responsibilities are implemented in separate packages. The network checks live in `internal/checks/`, the runner in `internal/runner/`, and the result and report model in `internal/diagnostic/`.

The runner, result aggregation, JSON serialization, and report exit-code mapping are implemented. The command-line interface and user-facing report rendering are not yet implemented.

## Individual Results

Each diagnostic result contains:

- `name`: a stable identifier for the check.
- `status`: the outcome of the check.
- `required`: whether the result contributes directly to the overall diagnostic status.
- `duration_ns`: the operation duration in nanoseconds when serialized as JSON.
- `message`: a concise description of the outcome.
- `error`: optional error information.
- `details`: optional structured evidence.

The internal implementation represents durations using Go's `time.Duration`. JSON serialization uses the integer field `duration_ns` to make the unit explicit.

The result model validates the check name, status, duration, and message. A result must have a non-empty name and message, a recognized status, and a non-negative duration.

Operational network outcomes are represented as diagnostic results when the check can produce a meaningful outcome. Invalid input or dependencies are returned as Go errors when a check cannot be executed correctly.

The runner also returns Go errors for invalid configuration and execution failures that prevent it from producing a valid report. This distinction prevents ordinary network outcomes from being confused with invalid configuration or internal errors.

### Result Statuses

#### Passed

The check satisfied its defined success condition.

A successful DNS lookup establishes that the resolver returned usable IP addresses. It does not establish that those addresses are reachable.

A successful TCP check establishes that a connection was opened to the specified IP address and port. It does not establish that TLS negotiation, application authentication, or an HTTP request will succeed.

A successful TLS check establishes that the TLS handshake completed and certificate verification succeeded under the configured trust settings. It does not establish that an HTTP endpoint or application function is healthy.

#### Failed

The check produced evidence that its success condition was not satisfied.

A DNS resolution failure is evidence that the attempted lookup did not produce a usable result.

A TCP connection error is evidence that the attempted connection was unsuccessful. It does not, by itself, establish why the remote endpoint failed to accept the connection.

A TLS failure means the TLS connection, handshake, or certificate verification did not succeed. The result alone does not necessarily establish the underlying cause.

The failed status describes the check's outcome, not a definitive explanation of the underlying fault.

#### Skipped

The check was not executed because it was not applicable or its prerequisites were unavailable.

A skipped check does not automatically establish a network failure. However, a required check that is skipped prevents NetProbe from claiming that all required checks passed.

The runner records skipped results in these situations:

- DNS does not produce usable addresses, so TCP checks cannot proceed.
- TLS is enabled but DNS produces no usable addresses.
- TLS is enabled but the corresponding TCP check does not pass.
- The diagnostic context has been canceled or has expired before a remaining address can be checked.

Skipped results include a message explaining why the check was not attempted. Where an address is known, the runner includes it in the result details.

Skipped results are preserved in the report and participate in aggregation according to their `required` value.

#### Inconclusive

The check ran but could not establish success or failure under its defined evaluation rules.

A timeout or cancellation may produce an inconclusive result when the operation did not complete within the available execution context.

The DNS check classifies context cancellation and deadline expiration as inconclusive outcomes.

The TCP check classifies context cancellation and deadline expiration as inconclusive. Ordinary connection failures remain failed outcomes.

The TLS check also classifies cancellation and deadline expiration as inconclusive. Other TLS connection, handshake, and certificate-verification failures are represented as failed outcomes.

The distinction between failed and inconclusive must follow the semantics of the individual check. Inconclusive must not be used to disguise an ordinary connection failure or an internal programming error.

## Required and Optional Checks

A result's `required` field determines whether its status contributes directly to the overall diagnostic status.

Required checks represent the conditions that must be satisfied for the diagnostic to be considered successful.

Optional results provide additional information. They remain visible in the report, but an optional failure does not override an otherwise successful set of required checks.

The current DNS, TCP, and TLS check implementations mark their results as required.

TLS is optional at the runner-configuration level: it is disabled by default and can be enabled through `Config.EnableTLS`. When enabled, the runner treats each produced TLS result as required. A TLS result skipped because its prerequisite did not pass is also marked required.

This distinction matters: optional configuration does not mean optional report status. With TLS enabled, a failed, inconclusive, or skipped TLS result can affect the overall report.

The current runner does not expose a separate configuration option to make TLS results optional in report aggregation.

A result must not be marked optional merely to make an unsuccessful diagnostic return success. Any future required-check policy should be explicit and documented.

## Overall Status

The report derives its overall status from the complete set of results.

Aggregation follows these rules in order:

1. If any required check failed, the overall status is `failed`.
2. Otherwise, if any required check was inconclusive or skipped, the overall status is `inconclusive`.
3. If there are no required checks, the overall status is `inconclusive`.
4. Otherwise, all required checks passed and the overall status is `passed`.

An empty result set is therefore inconclusive.

Optional results do not change the overall status when all required checks pass. They are still retained in the report and remain available to users and programmatic consumers.

A failed required check takes precedence over an inconclusive or skipped required check. This ordering ensures that an established failure is not concealed by another check that could not complete.

The aggregation logic is implemented in `internal/diagnostic/report.go`. The report constructor validates individual results, retains the supplied result collection, and derives the overall status. Report validation checks that the stored status agrees with the aggregation policy.

## Partial Success and Address Isolation

NetProbe preserves the result of each individual check.

For example, a target may have working IPv4 connectivity and failing IPv6 connectivity. The report must retain both observations rather than collapsing them into a single Boolean value.

DNS resolution runs once per diagnostic invocation. The DNS check retains usable IPv4 and IPv6 addresses separately, normalizes IPv4-mapped IPv6 addresses, removes duplicates, and sorts the resulting addresses for deterministic output.

The runner performs a TCP check for each usable address. A failure against one address does not automatically prevent attempts against the remaining addresses. Cancellation or expiration of the parent context affects subsequent attempts.

When TLS is enabled, the runner performs a TLS check only after the corresponding TCP check passes. The TLS check opens its own connection, so the TCP and TLS results represent separate observations.

The runner preserves individual results in execution order. It does not treat success against one address as proof that every resolved address is reachable.

Because the current checks mark their results as required, a failed required check for one address can make the overall report fail even when another address succeeds. The aggregation policy does not silently erase that failure.

## Diagnostic Runner

The runner is implemented in `internal/runner/runner.go`.

It validates the diagnostic context and configuration, resolves the target, executes the applicable checks, preserves individual outcomes, and builds a report through the diagnostic model.

The runner configuration includes:

- Target hostname.
- Destination port.
- DNS timeout.
- TCP timeout.
- TLS timeout.
- TLS enablement.
- An optional TLS root certificate pool.
- Resolver and dialer dependencies.

The target must not be empty. The port must be within the valid TCP port range. DNS and TCP timeouts must be greater than zero, and the TLS timeout must be greater than zero when TLS is enabled. The context, resolver, and dialer must not be nil.

DNS resolution is performed once. If DNS does not produce usable addresses, the runner preserves the DNS result and adds skipped results for dependent checks.

For each usable address, the runner attempts TCP connectivity. If TLS is enabled and the TCP check passes, the runner performs a TLS handshake and certificate verification using the original target hostname.

The runner bounds network operations with configured timeouts and the parent context. An earlier parent-context deadline takes precedence over a configured timeout.

The runner returns configuration or execution errors as Go errors when it cannot produce a valid report. Ordinary network outcomes remain in the diagnostic result collection.

## Report Structure and JSON Serialization

A report contains:

- `target`: the requested diagnostic target.
- `started_at`: the diagnostic start timestamp.
- `duration_ns`: total diagnostic duration in nanoseconds when serialized as JSON.
- `status`: the derived overall status.
- `results`: the ordered collection of individual results.

The report model validates its basic fields and individual results. It also checks that the stored overall status agrees with the aggregation policy.

Individual results preserve execution order so that consumers can interpret the sequence of checks consistently.

The report and individual result types implement JSON serialization. Durations are emitted as integer nanosecond values through fields named `duration_ns`; the internal Go representation remains `time.Duration`.

JSON serialization support does not mean a complete command-line JSON output mode exists. The CLI and user-facing presentation layer remain future work.

The report model retains evidence produced by the checks. It does not independently perform network operations or infer a root cause from the result collection.

## Process Exit Codes

The report model implements `ExitCode()`, which maps the overall report status to a process exit-code value.

| Exit code | Meaning |
|---|---|
| `0` | All required checks passed. |
| `1` | A required check failed, the report was inconclusive, or required checks did not establish success. |
| `2` | Reserved for invalid command-line arguments or configuration errors handled by the CLI. |
| `3` | Reserved for internal errors that prevent production of a valid diagnostic report. |

The current `ExitCode()` method maps a passed report to `0`, failed or inconclusive reports to `1`, and an unrecognized report status to `3`.

Exit codes `2` and `3` describe the intended CLI contract for errors handled outside ordinary check results. They are not separate network-check statuses. The report model does not terminate the process.

The command-line interface has not yet been implemented, so these values are not yet exposed through a complete user-facing command.

## Evidence and Interpretation

NetProbe distinguishes observations from interpretations.

An observation describes what a check directly established. An interpretation explains what those observations may imply. A root-cause claim requires sufficient evidence to identify the underlying fault.

For example:

- Observation: the host resolved an AAAA record.
- Observation: a TCP connection to the selected IPv6 destination failed.
- Possible interpretation: IPv6 connectivity may be unavailable along the tested path.
- Root cause: not established by those observations alone.

Other examples illustrate the same boundary:

- A successful DNS lookup does not prove that a destination is reachable.
- A successful TCP connection does not prove that the application protocol is healthy.
- A TCP connection failure does not uniquely identify whether the cause lies in the local host, routing path, firewall policy, remote host, or service.
- A TLS verification failure does not automatically identify which configuration or trust issue caused it.
- A timeout establishes that an operation did not complete within the available time limit. It does not necessarily establish that the destination is unreachable.

Diagnostic messages should describe the operation and its outcome without asserting causes that the collected evidence cannot establish.

Structured details should retain useful evidence for further inspection. They should not be treated as proof of a root cause simply because they are machine-readable.

## Implementation Boundaries

The current implementation is organized into three primary layers:

- `internal/checks/` executes individual DNS, TCP, and TLS operations.
- `internal/runner/` sequences checks, applies dependencies, and preserves partial results.
- `internal/diagnostic/` validates results, aggregates report status, serializes JSON, and maps report status to an exit-code value.

The command-line interface, HTTP response checks, and user-facing report rendering are not yet implemented.

Changes to result statuses, required-check semantics, aggregation rules, TLS behavior, or exit-code mapping must be reflected in the implementation, tests, and documentation.

Unit tests should remain deterministic and must not depend on public Internet access for routine verification.
