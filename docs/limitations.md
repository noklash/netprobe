# Limitations

## Purpose

NetProbe reports evidence from specific connectivity attempts. Each result describes the tested target, protocol, path as observed from the execution environment, and observation period. Results are not a universal verdict on network health, nor do they necessarily identify the root cause of a failure.

NetProbe distinguishes between an observed failure, an inconclusive attempt, and a successful check. The meaning of each result depends on what the check actually tested and what evidence it collected.

## Time-Bound Results

Network conditions change over time. DNS answers, routing tables, firewall rules, remote service availability, and upstream network paths may change after a diagnostic completes.

A previous success does not guarantee future connectivity. Likewise, a failure observed during one attempt does not establish that the same destination will remain unreachable.

Each result should be interpreted in the context of its observation time and configured timeout.

## DNS

Successful DNS resolution proves that the configured resolver returned one or more usable IP addresses for the queried hostname. It does not prove that those addresses are reachable or that a service is listening on them.

A failed lookup does not, by itself, distinguish among resolver failure, authoritative name-server problems, filtering, transient packet loss, hostname configuration errors, or other causes.

The DNS check records the addresses returned by the resolver after filtering invalid addresses, normalizing address representations, and removing duplicates. Its output describes the observed resolution result, not the reachability of every returned destination.

DNS answers may vary between resolvers, networks, and observation times. A result from one resolver should not be treated as proof of what every resolver would return.

## Address Families

IPv4 and IPv6 are distinct network paths and must be evaluated independently.

Failure to reach one or more IPv6 destinations does not prove that all IPv6 connectivity is unavailable. Likewise, the absence of an IPv6 address in a DNS response does not establish that the host lacks IPv6 connectivity in general.

The DNS check records IPv4 and IPv6 answers separately. The TCP check tests a specific IP address and port; it does not independently establish the general availability of an entire address family.

A future orchestration layer must preserve address-family-specific results rather than collapsing separate attempts into an unsupported conclusion about overall IPv4 or IPv6 availability.

## TCP

A successful TCP check proves that a connection was established to a particular destination IP address and port during the observation period. It does not prove that the application protocol above TCP is functioning correctly or that the remote service can complete its intended work.

A failed connection may result from filtering, routing problems, a service that is not listening, resource limits, timeouts, or other transport-level conditions. An error alone does not necessarily identify the root cause.

An inconclusive result indicates that the check could not establish a reliable pass-or-fail conclusion, such as when its context was cancelled or its deadline expired. It should not automatically be interpreted as proof that the destination is unreachable.

The TCP check depends on the supplied dialer respecting the provided context. The standard context-aware network dialer supports this behavior, but a custom dialer that ignores cancellation or deadlines may not.

A successful TCP handshake is evidence of transport connectivity only. Application-level checks are required to establish whether a service responds correctly.

## TLS

TLS failures may involve certificate verification, hostname mismatch, certificate expiration, protocol compatibility, server configuration, interception, or underlying transport behavior.

A TLS failure should be reported with the evidence available from the handshake and verification process. It should not automatically be attributed to a certificate problem when the underlying cause has not been established.

Disabling certificate verification is not an acceptable default workaround. Any future TLS check must preserve normal certificate and hostname verification unless an explicitly documented, narrowly scoped test requires different behavior.

## HTTP

An HTTP response proves that an HTTP endpoint responded to a request. It does not establish that every application function is healthy, that all dependencies are available, or that the response represents the expected business outcome.

HTTP status-code interpretation must follow explicitly documented success criteria. A response should not be considered successful merely because the server returned a status code.

Future HTTP checks should distinguish transport errors, TLS errors, HTTP status outcomes, and request-level timeouts where the available evidence permits.

## Environment

The execution environment can materially affect diagnostic results. Virtual machines, containers, VPNs, proxies, firewalls, host-side routing, DNS configuration, and other network settings may change the path or behavior observed by NetProbe.

A guest-visible route or gateway does not reveal every detail of the forwarding path beyond the guest. The guest operating system may expose only part of the network configuration that determines whether a connection succeeds.

Results therefore describe connectivity from the environment in which NetProbe runs. They should not be generalized automatically to the host operating system, another guest, another network interface, or a remote system.

The initial IPv6 experiment documents one observed environment. It does not establish a general VirtualBox defect or prove that the same behavior will occur under different host, guest, adapter, or network configurations.

## Root-Cause Attribution

NetProbe is an evidence-collection tool. Its primary responsibility is to perform defined checks, preserve their outcomes, and report what those outcomes support.

A failed check is not, by itself, a root-cause diagnosis. Further investigation may require additional checks, packet captures, operating-system network state, router or hypervisor logs, firewall configuration, resolver logs, or access to systems outside the guest.

NetProbe should distinguish observed facts from possible explanations. When the available evidence cannot identify a cause, the result and documentation should state that limitation explicitly.

## Security and Scope

The initial tool focuses on outbound connectivity checks initiated by the user.

NetProbe should not modify network configuration, disable certificate verification, or silently invoke privileged commands. Diagnostic behavior should remain explicit and bounded by the user's request and the configured timeouts.

Network checks should use context-aware operations where supported, release acquired resources, and avoid leaving connections or other resources open after a check completes.

Future checks must document any additional permissions, network activity, or security implications they introduce. The tool should not imply that a check is passive when performing it initiates a connection or sends a request to a remote system.

## Implementation Boundaries

The documented limitations describe both the nature of network diagnostics and the current implementation boundary.

The DNS and TCP checks are implemented. TLS and HTTP checks, command-line orchestration, and end-to-end diagnostic execution remain future work until implemented and verified.

Documented future behavior must not be mistaken for an existing capability. As NetProbe develops, this file should be updated when new checks introduce additional limitations or when implementation and testing provide evidence that a limitation has been addressed.