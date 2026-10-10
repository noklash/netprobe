# Limitations

## Purpose

NetProbe reports evidence from specific connectivity attempts. Results describe the tested target, protocol, path, and observation period. They are not a universal verdict on network health.

## Time-Bound Results

Network conditions change. DNS answers, routes, firewall rules, remote services, and upstream paths may change after a diagnostic completes.

A previous success does not guarantee future connectivity.

## DNS

Successful DNS resolution proves that a resolver returned an answer. It does not prove that the returned addresses are reachable.

A failed lookup does not, by itself, distinguish among resolver failure, name-server problems, filtering, transient packet loss, or other causes.

## Address Families

Failure to reach one or more IPv6 destinations does not prove that all IPv6 connectivity is unavailable.

Likewise, the absence of an IPv6 address in a DNS response does not establish that the host lacks IPv6 connectivity in general.

IPv4 and IPv6 should be evaluated independently.

## TCP

A successful TCP connection proves that a connection was established to a particular destination and port at a particular time.

A failure may result from filtering, routing problems, a service that is not listening, resource limits, or timeouts. An error alone does not necessarily identify the root cause.

## TLS

TLS failures may involve certificate verification, hostname mismatch, protocol compatibility, server configuration, interception, or transport behavior.

Disabling certificate verification is not an acceptable default workaround.

## HTTP

An HTTP response proves that an HTTP endpoint responded to a request. It does not prove that every application function is healthy.

Status-code interpretation must follow explicitly documented success criteria.

## Environment

Virtual machines, containers, VPNs, proxies, firewalls, and host-side configuration can affect results.

A guest-visible route or gateway does not reveal every detail of the forwarding path beyond the guest.

The initial IPv6 experiment describes one observed environment. It does not establish a general VirtualBox defect.

## Root-Cause Attribution

NetProbe is an evidence-collection tool. Further tests, packet captures, router or hypervisor logs, and access to systems outside the guest may be needed to identify a root cause.

## Security and Scope

The initial tool should not modify network configuration, disable certificate verification, or silently invoke privileged commands.

The first milestone focuses on outbound connectivity checks initiated by the user.
