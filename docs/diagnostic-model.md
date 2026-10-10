# Diagnostic Model

## Purpose

NetProbe reports observations from individual connectivity checks. A successful early stage does not guarantee that later stages will succeed.

DNS can succeed while TCP fails. TCP can succeed while TLS fails. TLS can succeed while an HTTP server returns an error response.

## Diagnostic Stages

### Input validation

Validate the target and requested options before initiating network operations.

### DNS resolution

Record the queried hostname, returned addresses, address families, duration, and resolver errors where available.

Successful resolution proves that addresses were returned. It does not prove those addresses are reachable.

### Address-family classification

Classify returned addresses as IPv4 or IPv6. Preserve the destination address and family in subsequent results.

If the hostname has no address for one family, report that the family could not be tested for this target. Do not conclude that the host generally lacks connectivity through that family.

### TCP connection

Attempt a bounded connection to the selected destination and port.

Record the destination, address family, duration, outcome, and error details.

A successful TCP connection establishes connectivity to that destination and port at that time. It does not prove TLS or application-level success.

Connection refusal, timeout, and network-unreachable errors are distinct observations and should remain distinguishable where possible.

### TLS negotiation

For HTTPS targets, attempt TLS after TCP succeeds.

Record handshake success or failure, duration, and relevant protocol or certificate errors.

Use the intended hostname for SNI and certificate verification. Certificate verification must remain enabled by default.

A TLS failure does not automatically establish its root cause.

### HTTP response

For HTTP and HTTPS targets, send a bounded request when prerequisites permit.

Record whether a response was received, the status code, duration, and request errors.

An HTTP response demonstrates that an HTTP endpoint responded. It does not necessarily establish application health.

## Check Outcomes

Each check should support these outcomes:

- **Passed:** the defined success condition was observed.
- **Failed:** the check ran, but its success condition was not met.
- **Skipped:** the check was not run because it was inapplicable or a prerequisite was unavailable.
- **Inconclusive:** the available evidence was insufficient to determine the outcome.

Each result should include a concise explanation. Errors belong to the stage that produced them.

A skipped check is not a successful check.

## Partial Success

Preserve individual results when checks have different outcomes.

Examples:

- DNS succeeds, IPv4 TCP succeeds, and IPv6 TCP fails.
- TCP succeeds but TLS fails.
- TLS succeeds and the server returns HTTP 503.

Do not collapse these outcomes into a single ambiguous statement such as "the network failed."

## Overall Status

Derive the overall summary from individual results. The exact aggregation rules and process exit codes must be defined and tested before the first release.

The summary must not hide partial connectivity or claim an unsupported root cause.

## Timing

Use Go's monotonic timing facilities for measuring elapsed durations.

DNS, TCP, TLS, and HTTP durations must describe their respective operations. A timeout must be bounded and represented explicitly.

## Evidence and Conclusions

Keep these concepts distinct:

- **Observation:** what the check directly returned.
- **Interpretation:** what the observation supports.
- **Hypothesis:** a possible explanation requiring further tests.
- **Root cause:** an explanation supported by sufficient evidence.

For example, a gateway returning a network-unreachable error supports the conclusion that the tested destination was unreachable through that observed path at that time. It does not, by itself, locate the fault in the guest, hypervisor, host, gateway, or upstream network.
