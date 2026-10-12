# NetProbe

NetProbe is a Linux network connectivity diagnostic tool written in Go.

It investigates outbound connectivity by examining individual stages of a connection, including DNS resolution, address-specific TCP connection establishment, TLS negotiation, and HTTP responses.

The objective is to produce evidence that helps explain where a connection succeeds or fails, rather than reporting a single ambiguous network status.

NetProbe is being developed as an engineering project focused on understanding network behavior, building testable diagnostic components, and documenting the evidence behind its conclusions.

## Project Status

NetProbe is in early development.

The initial connectivity experiments are documented, and the foundational diagnostic model, report aggregation, DNS resolution check, and TCP connectivity check have been implemented.

The DNS and TCP checks are independently testable and support controlled testing without relying on the public Internet. The diagnostic model preserves individual outcomes and distinguishes successful, failed, skipped, and inconclusive checks.

The command-line interface and end-to-end diagnostic orchestration remain under development. TLS and HTTP checks are planned but have not yet been implemented.

## Initial Scope

The first milestone is a one-shot command-line diagnostic tool for investigating outbound connectivity.

Planned capabilities include:

- Resolve DNS records for a target hostname.
- Examine IPv4 and IPv6 connectivity independently.
- Measure TCP connection establishment to specific destination addresses and ports.
- Measure TLS handshake completion.
- Validate HTTP responses when testing HTTP or HTTPS endpoints.
- Apply bounded timeouts to network operations.
- Preserve individual check results, errors, and durations.
- Aggregate results without discarding evidence from individual checks.
- Produce human-readable and JSON output.
- Test core behavior without depending on the public Internet.

The initial implementation will use Go's standard library wherever practical. Additional dependencies will be introduced only when they provide a clear technical benefit.

## Design Principles

### Evidence First

NetProbe distinguishes observations from hypotheses and conclusions. A failed check does not automatically establish a root cause.

### Independent Checks

A failure in one stage must not erase successful results from another. DNS resolution, TCP connectivity, and future application-level checks should retain their individual outcomes.

### Explicit Address Families

IPv4 and IPv6 results must remain distinguishable. A failure involving one address or address family must not be generalized into an unsupported conclusion about all network connectivity.

### Bounded Execution

Network operations must have defined time limits and support cancellation where applicable. Timeouts and cancellations must be represented accurately rather than automatically classified as proof of unreachability.

### Reproducibility

Experiments should document their environment, commands, observations, and limitations. Conclusions must remain within the scope of the evidence collected.

### Testability

Core diagnostic behavior must be testable without public network access. Dependencies such as DNS resolvers and network dialers should be injectable where appropriate, allowing deterministic tests of success, failure, cancellation, and timeout behavior.

### Explicit Failure Semantics

NetProbe distinguishes between a check that passed, a check that failed, a check that was skipped, and a check whose outcome is inconclusive.

Overall status is derived from the individual results and their requirements. It must not conceal uncertainty or imply that every possible network path was tested.

### Focused Scope

NetProbe diagnoses outbound connectivity. It does not aim to duplicate Hostcheck's host inventory, resource collection, or system health assessment.

Hostcheck and NetProbe address different engineering questions: Hostcheck examines the condition of a Linux host, while NetProbe investigates whether specific network operations succeed from the environment where it runs.

## Current Implementation

The current implementation includes the following components.

| Component | Status | Responsibility |
|---|---|---|
| Diagnostic result model | Implemented | Represents individual check outcomes, durations, errors, and supporting details. |
| Diagnostic report aggregation | Implemented | Aggregates required check results into an overall diagnostic status. |
| DNS resolution | Implemented | Resolves a hostname, normalizes returned addresses, removes duplicates, and records IPv4 and IPv6 results separately. |
| TCP connectivity | Implemented | Attempts a TCP connection to a specified IP address and port using a bounded context. |
| TLS handshake check | Planned | Tests TLS negotiation and certificate verification. |
| HTTP response check | Planned | Tests HTTP endpoint responses against documented success criteria. |
| Diagnostic orchestration | In development | Coordinates checks and preserves individual results. |
| Command-line interface | Planned | Exposes diagnostic execution and user-facing options. |
| Human-readable and JSON output | Planned | Presents diagnostic evidence in usable output formats. |

Implemented components are not yet equivalent to a complete end-to-end diagnostic application.

## Repository Structure

The repository is organized around diagnostic models, independently testable network checks, documentation, and experiments.

```text
netprobe/
├── internal/
│   ├── checks/
│   │   ├── dns.go
│   │   ├── dns_test.go
│   │   ├── tcp.go
│   │   └── tcp_test.go
│   └── diagnostic/
│       ├── report.go
│       ├── report_test.go
│       ├── result.go
│       └── result_test.go
├── docs/
│   ├── architecture.md
│   ├── diagnostic-model.md
│   ├── limitations.md
│   └── experiments/
│       └── 001-ipv6-connectivity-baseline.md
├── go.mod
├── README.md
├── CHANGELOG.md
└── .gitignore