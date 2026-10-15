# Limitations

## Purpose

NetProbe reports evidence from specific connectivity attempts. Each result describes an operation performed from the execution environment, the destination tested, and the observation made during that attempt.

Results are not a universal verdict on network health. They do not necessarily identify the root cause of a failure.

NetProbe distinguishes between a successful check, an observed failure, an inconclusive attempt, and a check that could not be performed. The meaning of each result depends on what the check actually tested and what evidence it collected.

The current implementation includes DNS resolution, TCP connectivity checks, TLS handshake and certificate verification, diagnostic orchestration, result aggregation, and JSON serialization. HTTP checks, the command-line interface, and user-facing report rendering remain outside the current implementation.

## Time-Bound Results

Network conditions change over time. DNS answers, routing tables, firewall rules, remote service availability, and upstream network paths may change after a diagnostic completes.

A previous success does not guarantee future connectivity. Likewise, a failure observed during one attempt does not establish that the same destination will remain unreachable.

Each result should be interpreted in the context of its observation time, configured timeout, and execution environment.

The report records the diagnostic start time and total duration. Individual results record their own durations. These values describe when and how long the diagnostic operations ran; they do not guarantee that the underlying network conditions remained unchanged throughout or after the run.

## DNS

Successful DNS resolution proves that the configured resolver returned usable IP addresses for the queried hostname. It does not prove that those addresses are reachable or that a service is listening on them.

A failed lookup does not, by itself, distinguish among resolver failure, authoritative name-server problems, filtering, transient packet loss, hostname configuration errors, or other causes.

The DNS check records addresses returned by the resolver after filtering invalid addresses, normalizing IPv4-mapped IPv6 representations, and removing duplicates. IPv4 and IPv6 addresses are retained as distinct usable destinations.

DNS answers may vary between resolvers, networks, and observation times. A result from one resolver should not be treated as proof of what every resolver would return.

The runner resolves the target once per diagnostic invocation. It does not independently establish that every possible address for the hostname has been discovered.

If DNS produces no usable addresses, dependent TCP checks cannot proceed. The runner preserves the DNS outcome and records skipped results for dependent checks rather than treating those checks as successful.

## Address Families

IPv4 and IPv6 are distinct network paths and must be evaluated independently.

Failure to reach one or more IPv6 destinations does not prove that all IPv6 connectivity is unavailable. Likewise, the absence of an IPv6 address in a DNS response does not establish that the host lacks IPv6 connectivity in general.

The DNS check records usable IPv4 and IPv6 addresses separately. The runner performs TCP checks against individual resolved addresses, preserving the outcome of each attempt.

A successful connection to one address does not establish that other addresses are reachable. A failure against one address does not prevent the report from retaining successful results from other addresses.

The current checks mark their results as required. Consequently, a failed required check for one address can cause the overall report to fail even when another address succeeds. This policy preserves failures instead of allowing a successful address to conceal them.

These results describe the specific addresses returned and tested during the invocation. They do not establish the general availability of an entire address family.

## TCP

A successful TCP check proves that a connection was established to a particular destination IP address and port during the observation period. It does not prove that the application protocol above TCP is functioning correctly or that the remote service can complete its intended work.

A failed connection may result from filtering, routing problems, a service that is not listening, resource limits, timeouts, or other transport-level conditions. An error alone does not necessarily identify the root cause.

An inconclusive result indicates that the check could not establish a reliable pass-or-fail conclusion, such as when its context was cancelled or its deadline expired. It should not automatically be interpreted as proof that the destination is unreachable.

The TCP check depends on the supplied dialer respecting the provided context. The standard context-aware network dialer supports this behavior, but a custom dialer that ignores cancellation or deadlines may not.

The runner applies a configured timeout to TCP operations, subject to the parent context's deadline. Cancellation or expiration of the parent context can affect subsequent checks.

A successful TCP connection is evidence of transport connectivity only. Application-level checks are required to establish whether a service responds correctly.

## TLS

The TLS check is implemented. It establishes a TLS connection to a resolved IP address and verifies the remote certificate using the original target hostname. This preserves hostname verification and allows the hostname to be used for Server Name Indication (SNI).

The check enforces TLS 1.2 as the minimum protocol version. When no custom root certificate pool is supplied, certificate verification uses the system's trusted root certificates.

A successful TLS check establishes that the handshake completed and certificate verification succeeded under those settings. It does not establish that an HTTP endpoint or application function is healthy.

TLS failures may involve certificate verification, hostname mismatch, certificate expiration, protocol compatibility, server configuration, interception, or underlying transport behavior. A failure should not automatically be attributed to a certificate problem when the underlying cause has not been established.

The result includes available handshake evidence, such as the negotiated TLS version and cipher suite, and certificate-chain verification information. The error field may contain error text useful for investigation, but that text does not guarantee a definitive root-cause diagnosis.

The TLS check opens its own connection after the runner observes a successful TCP check. The TCP and TLS results therefore represent separate connection attempts, and network conditions can differ between them.

TLS is disabled by default in the runner configuration. When enabled, the runner performs TLS only after the corresponding TCP check passes. The current runner marks produced TLS results as required, so TLS failures can affect the overall diagnostic status.

Certificate verification should not be disabled as a workaround for a failed TLS check. The current implementation preserves normal certificate and hostname verification.

## HTTP

HTTP response checks are not implemented.

An HTTP response can establish that an HTTP endpoint responded to a request, but it does not establish that every application function is healthy, that all dependencies are available, or that the response represents the expected business outcome.

A future HTTP check must document its success criteria explicitly. It should distinguish transport errors, TLS errors, HTTP status outcomes, and request-level timeouts where the available evidence permits.

A response should not be considered successful merely because the server returned a status code.

Until an HTTP check is implemented and tested, NetProbe should not claim to verify HTTP response status, application behavior, or application-level health.

## Execution and Reporting

The diagnostic runner is implemented. It validates its configuration, resolves the target, executes dependent checks in sequence, preserves individual results, and builds an aggregate report.

The result and report model supports validation, status aggregation, JSON serialization, and exit-code mapping. These capabilities are available to Go callers and tests.

The command-line interface has not yet been implemented. NetProbe therefore does not yet provide a complete user-facing command for configuring and executing diagnostics.

User-facing report rendering is also not implemented. JSON serialization support in the diagnostic model should not be confused with a finished CLI JSON output mode.

The report's exit-code mapping is implemented in the diagnostic package, but there is no CLI entry point yet to parse arguments, handle process-level errors, print a final report, and terminate the process with the mapped code.

## Timeouts and Cancellation

DNS, TCP, and TLS checks use configured timeouts and the parent execution context to bound operations.

An earlier parent-context deadline takes precedence over a check's configured timeout. Cancellation and deadline expiration may result in inconclusive outcomes or skipped dependent checks, depending on when the cancellation occurs.

Prompt cancellation depends on the underlying operation and injected dependencies respecting the supplied context. A custom resolver or dialer that ignores context cancellation may not stop promptly.

Timeouts describe the time allowed for a particular operation. They do not establish that the destination is permanently unreachable or identify why the operation failed to complete.

The runner preserves results collected before cancellation where its execution path permits. Checks that cannot be attempted after cancellation are recorded as skipped rather than being represented as successful.

## Environment

The execution environment can materially affect diagnostic results. Virtual machines, containers, VPNs, proxies, firewalls, host-side routing, DNS configuration, and other network settings may change the path or behavior observed by NetProbe.

A guest-visible route or gateway does not reveal every detail of the forwarding path beyond the guest. The guest operating system may expose only part of the network configuration that determines whether a connection succeeds.

Results therefore describe connectivity from the environment in which NetProbe runs. They should not be generalized automatically to the host operating system, another guest, another network interface, or a remote system.

The initial IPv6 experiment documents one observed environment. It does not establish a general VirtualBox defect or prove that the same behavior will occur under different host, guest, adapter, or network configurations.

## Root-Cause Attribution

NetProbe is an evidence-collection tool. Its primary responsibility is to perform defined checks, preserve their outcomes, and report what those outcomes support.

A failed check is not, by itself, a root-cause diagnosis. Further investigation may require additional checks, packet captures, operating-system network state, router or hypervisor logs, firewall configuration, resolver logs, or access to systems outside the guest.

NetProbe should distinguish observed facts from possible explanations. When the available evidence cannot identify a cause, the result and documentation should state that limitation explicitly.

Structured results can help make observations reproducible and inspectable. Machine-readable evidence does not, by itself, establish why a failure occurred.

## Security and Scope

The current tool focuses on outbound connectivity checks initiated by the user.

NetProbe does not intentionally modify network configuration or silently invoke privileged commands. Its diagnostic operations initiate network activity and should not be described as entirely passive.

Network checks should use context-aware operations where supported, release acquired resources, and avoid leaving connections or other resources open after a check completes.

TLS certificate and hostname verification remain enabled. A custom root certificate pool can be supplied through the runner configuration when a specific trust configuration is required.

Future checks must document any additional permissions, network activity, or security implications they introduce.

## Implementation Boundaries

The current implementation includes:

- DNS resolution and usable-address filtering.
- TCP connectivity checks for individual resolved addresses.
- TLS handshake and certificate verification.
- Runner orchestration and dependent-check handling.
- Individual result validation and aggregate report status.
- JSON serialization for diagnostic results and reports.
- Report-level exit-code mapping.

The following capabilities remain planned:

- HTTP response checks.
- A command-line interface.
- User-facing report rendering and a complete command-line output workflow.

Documented future behavior must not be mistaken for an existing capability. As NetProbe develops, this file should be updated when new checks introduce additional limitations or when implementation and testing provide evidence that a limitation has been addressed.
