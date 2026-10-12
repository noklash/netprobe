package checks

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// Resolver abstracts DNS lookup so the check can be tested without
// depending on a live DNS server.
type Resolver interface {
	LookupNetIP(
		ctx context.Context,
		network string,
		host string,
	) ([]netip.Addr, error)
}

// ResolveDNS resolves a hostname and records the returned addresses.
//
// Invalid input and invalid dependencies are returned as Go errors.
// DNS failures are represented as diagnostic results so callers can
// preserve them in a report.
func ResolveDNS(
	ctx context.Context,
	resolver Resolver,
	hostname string,
) (diagnostic.Result, []netip.Addr, error) {
	if ctx == nil {
		return diagnostic.Result{}, nil, errors.New("context must not be nil")
	}

	if resolver == nil {
		return diagnostic.Result{}, nil, errors.New("resolver must not be nil")
	}

	if hostname == "" {
		return diagnostic.Result{}, nil, errors.New("hostname must not be empty")
	}

	startedAt := time.Now()

	addresses, err := resolver.LookupNetIP(ctx, "ip", hostname)
	duration := time.Since(startedAt)

	result := diagnostic.Result{
		Name:     "dns",
		Required: true,
		Duration: duration,
		Details: map[string]any{
			"hostname": hostname,
		},
	}

	if err != nil {
		result.Error = err.Error()

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			result.Status = diagnostic.StatusInconclusive
			result.Message = "DNS resolution did not complete within the available context"
		} else {
			result.Status = diagnostic.StatusFailed
			result.Message = "DNS resolution failed"
		}

		return result, nil, nil
	}

	// Unmap IPv4-mapped IPv6 addresses before classifying address families.
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))

	for _, address := range addresses {
		if !address.IsValid() {
			continue
		}

		address = address.Unmap()

		if _, exists := seen[address]; exists {
			continue
		}

		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Compare(normalized[j]) < 0
	})

	ipv4 := make([]string, 0, len(normalized))
	ipv6 := make([]string, 0, len(normalized))

	for _, address := range normalized {
		switch {
		case address.Is4():
			ipv4 = append(ipv4, address.String())
		case address.Is6():
			ipv6 = append(ipv6, address.String())
		}
	}

	result.Details["ipv4_addresses"] = ipv4
	result.Details["ipv6_addresses"] = ipv6
	result.Details["address_count"] = len(normalized)

	if len(normalized) == 0 {
		result.Status = diagnostic.StatusFailed
		result.Message = "DNS resolution returned no usable IP addresses"
		return result, nil, nil
	}

	result.Status = diagnostic.StatusPassed
	result.Message = fmt.Sprintf(
		"DNS resolution returned %d usable IP address(es)",
		len(normalized),
	)

	return result, normalized, nil
}
