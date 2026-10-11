# Diagnostic Model

## Purpose

NetProbe evaluates network connectivity through a sequence of diagnostic
checks. Each check produces an observation about a particular operation,
such as DNS resolution, a TCP connection, a TLS handshake, or an HTTP
request.

The diagnostic model preserves individual observations and derives an
overall status from the checks designated as required.

The model separates three concerns:

1. Individual check execution and evidence collection.
2. Aggregation of check results into an overall diagnostic status.
3. Translation of the overall status into a process exit code.

This separation allows the diagnostic policy to be tested independently
of network operations and command-line execution.

## Individual Results

Each diagnostic result contains:

- `name`: a stable identifier for the check.
- `status`: the outcome of the check.
- `required`: whether the check must pass for the overall diagnostic to
  succeed.
- `duration_ns`: the duration of the check in nanoseconds when serialized
  as JSON.
- `message`: a concise description of the outcome.
- `error`: optional error information.
- `details`: optional structured evidence.

The internal implementation represents durations using Go's
`time.Duration`. JSON output uses an integer field named `duration_ns`
to make the unit explicit.

### Result Statuses

#### Passed

The check satisfied its defined success condition.

A successful DNS lookup, for example, establishes that the resolver
returned addresses. It does not establish that those addresses are
reachable.

#### Failed

The check produced evidence that its success condition was not satisfied.

A TCP connection refusal is a concrete connection failure. It does not,
by itself, establish why the remote endpoint refused the connection.

#### Skipped

The check was not executed because it was not applicable or its
prerequisites were unavailable.

A skipped check does not automatically establish a network failure.
However, a required check that is skipped prevents NetProbe from claiming
that all required checks passed.

#### Inconclusive

The check ran but could not establish success or failure under its
defined evaluation rules.

A timeout may produce an inconclusive result when the available evidence
does not establish the underlying cause.

The distinction between failed and inconclusive outcomes must follow the
semantics of the individual check. It must not be used to disguise an
ordinary connection failure as an internal error.

## Required and Optional Checks

A result's `required` field determines whether its status contributes
directly to the overall diagnostic status.

Required checks represent the conditions that must be satisfied for the
diagnostic to be considered successful.

Optional checks provide additional information. Their results remain
visible in the report, but an optional failure does not override an
otherwise successful set of required checks.

The required-check policy must be established by the diagnostic
configuration and documented for each supported invocation.

A result must not be marked optional merely to make an unsuccessful
diagnostic return success.

## Overall Status

The report derives its overall status from the complete set of results.

Aggregation follows these rules in order:

1. If any required check failed, the overall status is `failed`.
2. Otherwise, if any required check was inconclusive or skipped, the
   overall status is `inconclusive`.
3. If there are no required checks, the overall status is `inconclusive`.
4. Otherwise, all required checks passed and the overall status is
   `passed`.

An empty result set is therefore inconclusive.

Optional results do not change the overall status when all required
checks pass. They are still retained in the report and must remain
available to users and programmatic consumers.

### Partial Success

NetProbe must preserve the result of each individual check.

For example, a target may have working IPv4 connectivity and failing
IPv6 connectivity. The report must retain both observations rather than
collapsing them into a single Boolean value.

The overall status depends on which checks are required for the
invocation. The result collection must not discard useful partial success
or conceal an individual failure.

## Process Exit Codes

The CLI maps the overall report status to a process exit code.

| Exit code | Meaning |
|---|---|
| `0` | All required, applicable checks passed. |
| `1` | A required check failed, the result was inconclusive, or no required checks established success. |
| `2` | Invalid command-line arguments or configuration. |
| `3` | An internal error prevented production of a valid diagnostic report. |

Exit codes `2` and `3` are reserved for errors handled by the CLI layer.
They are not ordinary network-check outcomes.

A failed TCP connection, for example, should normally appear as a valid
diagnostic result and contribute to exit code `1` when that check is
required. It should not be treated as an internal execution error.

The diagnostic package does not terminate the process. It returns the
report and exposes the exit-code decision. Process termination belongs
to the CLI entry point.

## Report Structure

A report contains:

- `target`: the requested diagnostic target.
- `started_at`: the diagnostic start timestamp.
- `duration_ns`: the total diagnostic duration in nanoseconds.
- `status`: the derived overall status.
- `results`: the ordered collection of individual results.

The report validates its basic fields and individual results. It also
checks that the stored overall status agrees with the aggregation policy.

Individual result ordering should reflect the diagnostic execution plan
so that consumers can interpret the sequence consistently.

## Evidence and Interpretation

NetProbe distinguishes observations from interpretations.

An observation describes what a check directly established. An
interpretation explains what those observations may imply. A root-cause
claim requires sufficient evidence to identify the underlying fault.

For example:

- Observation: the host resolved an AAAA record.
- Observation: a TCP connection to the selected IPv6 destination failed.
- Possible interpretation: IPv6 connectivity may be unavailable along
  the tested path.
- Root cause: not established by those observations alone.

The diagnostic model must not imply that DNS success proves reachability,
that gateway reachability proves external connectivity, or that a
connection failure uniquely identifies the responsible network component.

## Implementation Boundaries

The diagnostic model is independent of network operations.

Network checks are responsible for executing bounded operations and
producing individual results. The diagnostic layer retains those results
and derives the overall status. The reporting layer serializes the
report, while the CLI handles argument errors, internal failures, and
process exit.

Unit tests for result validation and aggregation must not depend on public
Internet access.

Changes to result statuses, required-check semantics, aggregation rules,
or exit codes must be reflected in both the tests and this document.