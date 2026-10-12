# Diagnostic Model

## Purpose

NetProbe evaluates network connectivity through a sequence of diagnostic checks. Each check produces an observation about a particular operation, such as DNS resolution, a TCP connection, a TLS handshake, or an HTTP request.

The diagnostic model preserves individual observations and derives an overall status from the checks designated as required.

The model separates three concerns:

1. Individual check execution and evidence collection.
2. Aggregation of check results into an overall diagnostic status.
3. Translation of the overall status into a process exit code.

This separation allows diagnostic policy to be tested independently of network operations and command-line execution.

The result model and report aggregation are implemented. DNS resolution and TCP connectivity checks are also implemented as independent operations. The complete diagnostic runner, reporting layer, and command-line interface remain future work.

## Individual Results

Each diagnostic result contains:

- `name`: a stable identifier for the check.
- `status`: the outcome of the check.
- `required`: whether the check must pass for the overall diagnostic to succeed.
- `duration_ns`: the duration of the check in nanoseconds when serialized as JSON.
- `message`: a concise description of the outcome.
- `error`: optional error information.
- `details`: optional structured evidence.

The internal implementation represents durations using Go's `time.Duration`. JSON output uses an integer field named `duration_ns` to make the unit explicit.

The result model validates the check name, status, duration, and message. A result must have a non-empty name and message, a recognized status, and a non-negative duration.

Operational failures are represented as diagnostic results when the check can produce a meaningful outcome. Invalid input or dependencies are returned as Go errors when the check cannot be executed correctly.

This distinction prevents ordinary network failures from being confused with invalid configuration or programming errors.

### Result Statuses

#### Passed

The check satisfied its defined success condition.

A successful DNS lookup establishes that the resolver returned usable IP addresses. It does not establish that those addresses are reachable.

A successful TCP check establishes that a connection was opened to the specified IP address and port. It does not establish that TLS negotiation, application authentication, or an HTTP request will succeed.

#### Failed

The check produced evidence that its success condition was not satisfied.

A DNS resolution failure is evidence that the attempted lookup did not produce a usable result.

A TCP connection refusal or another connection error is evidence that the attempted connection was unsuccessful. It does not, by itself, establish why the remote endpoint refused or failed to accept the connection.

The failed status describes the check's outcome, not a definitive explanation of the underlying fault.

#### Skipped

The check was not executed because it was not applicable or its prerequisites were unavailable.

A skipped check does not automatically establish a network failure. However, a required check that is skipped prevents NetProbe from claiming that all required checks passed.

For example, if DNS resolution produces no usable address, a dependent TCP check cannot be performed against a resolved destination. The eventual diagnostic runner must preserve the DNS result and explain why the dependent check was skipped.

The current DNS and TCP functions execute individual checks. Consistent handling of skipped dependent checks belongs to the orchestration layer.

#### Inconclusive

The check ran but could not establish success or failure under its defined evaluation rules.

A timeout or cancellation may produce an inconclusive result when the operation did not complete within the available execution context.

The DNS check classifies context cancellation and deadline expiration as inconclusive outcomes.

The TCP check also classifies context cancellation and deadline expiration as inconclusive. Ordinary connection failures remain failed outcomes.

The distinction between failed and inconclusive must follow the semantics of the individual check. Inconclusive must not be used to disguise an ordinary connection failure or an internal programming error.

## Required and Optional Checks

A result's `required` field determines whether its status contributes directly to the overall diagnostic status.

Required checks represent the conditions that must be satisfied for the diagnostic to be considered successful.

Optional checks provide additional information. Their results remain visible in the report, but an optional failure does not override an otherwise successful set of required checks.

The required-check policy must be established by the diagnostic configuration and documented for each supported invocation.

A result must not be marked optional merely to make an unsuccessful diagnostic return success.

The current DNS and TCP checks mark their results as required. The eventual runner and configuration layer must determine whether that policy is appropriate for each diagnostic invocation, particularly when evaluating multiple addresses or optional protocol checks.

## Overall Status

The report derives its overall status from the complete set of results.

Aggregation follows these rules in order:

1. If any required check failed, the overall status is `failed`.
2. Otherwise, if any required check was inconclusive or skipped, the overall status is `inconclusive`.
3. If there are no required checks, the overall status is `inconclusive`.
4. Otherwise, all required checks passed and the overall status is `passed`.

An empty result set is therefore inconclusive.

Optional results do not change the overall status when all required checks pass. They are still retained in the report and must remain available to users and programmatic consumers.

A failed required check takes precedence over an inconclusive or skipped required check. This ordering ensures that an established failure is not concealed by another check that could not complete.

## Partial Success

NetProbe must preserve the result of each individual check.

For example, a target may have working IPv4 connectivity and failing IPv6 connectivity. The report must retain both observations rather than collapsing them into a single Boolean value.

DNS resolution can return both IPv4 and IPv6 addresses. The DNS check retains the usable addresses by family and removes duplicates. TCP checks operate on individual IP addresses, allowing each selected destination to be tested independently.

A successful TCP connection to one address does not establish that other addresses are reachable. Similarly, a successful DNS lookup does not establish that any returned address accepts connections.

The overall status depends on which checks are required for the invocation. The result collection must not discard useful partial success or conceal an individual failure.

The diagnostic runner must define how per-address results contribute to the overall status. That policy should preserve address-family isolation and must not allow success on one address to silently erase failure on another required address.

## Process Exit Codes

The CLI maps the overall report status to a process exit code.

| Exit code | Meaning |
|---|---|
| `0` | All required, applicable checks passed. |
| `1` | A required check failed, the result was inconclusive, or no required checks established success. |
| `2` | Invalid command-line arguments or configuration. |
| `3` | An internal error prevented production of a valid diagnostic report. |

Exit codes `2` and `3` are reserved for errors handled by the CLI layer. They are not ordinary network-check outcomes.

A failed TCP connection, for example, should normally appear as a valid diagnostic result and contribute to exit code `1` when that check is required. It should not be treated as an internal execution error.

The diagnostic package does not terminate the process. It returns the report and exposes the exit-code decision. Process termination belongs to the CLI entry point.

The exit-code mapping is the intended CLI contract. The CLI entry point and its process-termination behavior have not yet been implemented.

## Report Structure

A report contains:

- `target`: the requested diagnostic target.
- `started_at`: the diagnostic start timestamp.
- `duration_ns`: the total diagnostic duration in nanoseconds.
- `status`: the derived overall status.
- `results`: the ordered collection of individual results.

The report validates its basic fields and individual results. It also checks that the stored overall status agrees with the aggregation policy.

Individual result ordering should reflect the diagnostic execution plan so that consumers can interpret the sequence consistently.

The report model is responsible for retaining the evidence produced by individual checks. It must not independently perform network operations or infer a root cause from the result collection.

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
- A timeout establishes that an operation did not complete within the available time limit. It does not necessarily establish that the destination is unreachable.

Diagnostic messages should describe the operation and its outcome without asserting causes that the collected evidence cannot establish.

Structured details should retain useful evidence for further inspection. They should not be treated as proof of a root cause simply because they are machine-readable.

## Implementation Boundaries

The diagnostic model is independent of network operations.

Network checks are responsible for executing bounded operations and producing individual results. The diagnostic layer retains those results and derives the overall status. The reporting layer serializes the report, while the CLI handles argument errors, internal failures, and process exit.

The implemented DNS and TCP checks accept injected dependencies so that their core behavior can be tested without depending on public Internet access.

The DNS check accepts a resolver abstraction. The TCP check accepts a dialer abstraction and operates on a literal IP address and port, keeping DNS resolution separate from connection establishment.

The TCP check uses a timeout derived from its parent context. An earlier parent-context deadline takes precedence over the check's configured timeout. Prompt cancellation depends on the supplied dialer respecting the context.

The current checks produce individual results. The runner that sequences them, manages shared execution limits, handles dependencies, and preserves partial results remains future work.

Unit tests for result validation, aggregation, DNS behavior, and TCP behavior must not depend on public Internet access.

Changes to result statuses, required-check semantics, aggregation rules, or exit codes must be reflected in both the tests and this document.