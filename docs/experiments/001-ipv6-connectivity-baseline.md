# Experiment 001: IPv6 Connectivity Baseline

## Purpose

Investigate the difference between configured network routes and
actual outbound connectivity from an Ubuntu virtual machine.

The investigation compares IPv4 and IPv6 behavior across route
selection, gateway reachability, DNS resolution, TCP connectivity,
TLS negotiation, and HTTPS responses.

## Environment

- Operating system: Ubuntu 26.04
- Virtualization: VirtualBox
- Network interface: enp0s3
- IPv4 address: 10.0.2.15/24
- IPv4 gateway: 10.0.2.2
- IPv6 address: fd17:625c:f037:2:a00:27ff:fed0:dc4d/64
- IPv6 gateway: fe80::2
- IPv4 connectivity: Working during the experiment
- IPv6 external connectivity: Failing during the experiment

## Experiment 001: Route Selection and Connectivity

### IPv4

Linux selected the default gateway 10.0.2.2 through enp0s3.

ICMP echo requests to 1.1.1.1 succeeded:
- Packets transmitted: 3
- Packets received: 3
- Packet loss: 0%
- Average round-trip time: approximately 169.7 ms

HTTPS to example.com succeeded:
- HTTP status: 200
- TCP connection: approximately 202.6 ms
- TLS handshake: approximately 584.8 ms
- Total time: approximately 786.3 ms

### IPv6

Linux selected fe80::2 as the next hop for the tested public
IPv6 destination.

ICMP echo requests to 2606:4700:4700::1111 failed with:

    Destination unreachable: No route

HTTPS over IPv6 also failed before a TCP connection was established.

### DNS

DNS A and AAAA queries for example.com both returned successful
responses. The system could resolve both IPv4 and IPv6 addresses.

DNS resolution therefore succeeded even though external IPv6
connectivity failed.

## Experiment 002: IPv6 Gateway Diagnostics

The interface had a Unique Local Address (ULA) and a link-local
IPv6 address.

The IPv6 routing table contained a default route through fe80::2.

The neighbor table showed the gateway as REACHABLE. Direct ICMP
echo requests to fe80::2 succeeded:
- Packets transmitted: 3
- Packets received: 3
- Packet loss: 0%
- Average round-trip time: approximately 0.7 ms

The guest could therefore communicate with its IPv6 gateway.

However, gateway reachability did not establish external IPv6
connectivity.

## Experiment 003: IPv6 External Reachability

A second public IPv6 destination, 2001:4860:4860::8888, was tested.

ICMP echo requests failed with:

    Destination unreachable: No route

HTTPS to www.google.com over IPv6 failed before TCP connection
establishment.

The tracepath output included:

    fe80::2%enp0s3 !N

This is consistent with a network-unreachable response from the
gateway.

## Findings

1. IPv4 route selection and external connectivity worked.
2. IPv4 HTTPS returned HTTP 200.
3. DNS resolution returned both A and AAAA records.
4. The guest had an IPv6 address and a default IPv6 route.
5. The IPv6 gateway responded to ICMP echo requests.
6. Two tested public IPv6 destinations were unreachable.
7. IPv6 HTTPS failed before TCP connection establishment.
8. A configured default route did not guarantee external
   IPv6 connectivity.

## Current Diagnosis

The evidence indicates that the guest can reach its IPv6 gateway,
but the gateway reports no usable route to the tested public IPv6
destinations.

The failure is consistent with an IPv6 forwarding or upstream
routing limitation in the virtual machine's network path.

The exact fault location has not been established. Further evidence
would be required to distinguish a VirtualBox networking limitation
from a host-side or upstream routing problem.

## Engineering Lessons

- Route selection is not proof of reachability.
- Gateway reachability is not proof of external connectivity.
- DNS resolution is not proof that a resolved address is reachable.
- IPv4 and IPv6 must be tested independently.
- A useful diagnostic must preserve successful checks when other
  checks fail.
- Findings must distinguish direct observations from inferred causes.

## Reproducibility

The experiment used standard Linux networking utilities:
- ip
- ping
- dig
- curl
- tracepath

Results describe the observed environment at the time of testing.
They are not a guarantee that the same network behavior will persist.