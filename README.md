# NetProbe

NetProbe is a Linux network connectivity diagnostic tool written in Go.

It investigates outbound connectivity by examining individual stages of a connection, including DNS resolution, address-family selection, TCP connection establishment, TLS negotiation, and HTTP responses.

The objective is to produce evidence that helps explain where a connection succeeds or fails, rather than reporting a single ambiguous network status.

## Project Status

Early development. The initial connectivity experiments are documented, and the diagnostic model and architecture are being defined before implementation.

## Initial Scope

The first milestone is a one-shot command-line diagnostic tool.

Planned capabilities:

- Resolve DNS records for a target hostname.
- Examine IPv4 and IPv6 connectivity independently.
- Measure TCP connection establishment.
- Measure TLS handshake completion.
- Validate HTTP responses when testing HTTPS or HTTP endpoints.
- Apply bounded timeouts to network operations.
- Preserve individual check results, errors, and durations.
- Produce human-readable and JSON output.
- Test core behavior without depending on the public Internet.

The initial implementation will use Go's standard library wherever practical.

## Design Principles

- **Evidence first:** distinguish observations from hypotheses and conclusions.
- **Independent checks:** a failure in one stage must not erase successful results from another.
- **Explicit address families:** IPv4 and IPv6 results must remain distinguishable.
- **Bounded execution:** network operations must have defined time limits.
- **Reproducibility:** document test conditions, commands, observations, and limitations.
- **Testability:** core diagnostic behavior must be testable without public network access.
- **Focused scope:** NetProbe diagnoses outbound connectivity. It does not aim to duplicate Hostcheck's host inventory and health assessment.

## Repository Structure

The initial structure will evolve as implementation requirements become clearer.

```text
netprobe/
├── cmd/
│   └── netprobe/
│       └── main.go
├── internal/
│   ├── diagnostic/
│   ├── checks/
│   └── report/
├── docs/
│   ├── architecture.md
│   ├── diagnostic-model.md
│   ├── limitations.md
│   └── experiments/
├── go.mod
├── README.md
├── CHANGELOG.md
└── .gitignore
