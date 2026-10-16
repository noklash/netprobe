package checks

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/noklash/netprobe/internal/diagnostic"
)

type fakeResolver struct {
	addresses []netip.Addr
	err       error
}

func (f fakeResolver) LookupNetIP(
	_ context.Context,
	_ string,
	_ string,
) ([]netip.Addr, error) {
	return f.addresses, f.err
}

func TestResolveDNS(t *testing.T) {
	tests := []struct {
		name           string
		addresses      []netip.Addr
		resolverErr    error
		wantStatus     diagnostic.Status
		wantAddressCnt int
		wantIPv4       []string
		wantIPv6       []string
	}{
		{
			name: "IPv4 and IPv6 addresses",
			addresses: []netip.Addr{
				netip.MustParseAddr("2001:db8::10"),
				netip.MustParseAddr("192.0.2.10"),
			},
			wantStatus:     diagnostic.StatusPassed,
			wantAddressCnt: 2,
			wantIPv4:       []string{"192.0.2.10"},
			wantIPv6:       []string{"2001:db8::10"},
		},
		{
			name: "IPv4 only",
			addresses: []netip.Addr{
				netip.MustParseAddr("192.0.2.20"),
			},
			wantStatus:     diagnostic.StatusPassed,
			wantAddressCnt: 1,
			wantIPv4:       []string{"192.0.2.20"},
			wantIPv6:       []string{},
		},
		{
			name: "IPv6 only",
			addresses: []netip.Addr{
				netip.MustParseAddr("2001:db8::20"),
			},
			wantStatus:     diagnostic.StatusPassed,
			wantAddressCnt: 1,
			wantIPv4:       []string{},
			wantIPv6:       []string{"2001:db8::20"},
		},
		{
			name:        "resolver error",
			resolverErr: errors.New("DNS server unavailable"),
			wantStatus:  diagnostic.StatusFailed,
		},
		{
			name:        "context deadline",
			resolverErr: context.DeadlineExceeded,
			wantStatus:  diagnostic.StatusInconclusive,
		},
		{
			name:       "no addresses returned",
			addresses:  []netip.Addr{},
			wantStatus: diagnostic.StatusFailed,
		},
		{
			name: "invalid addresses are ignored",
			addresses: []netip.Addr{
				{},
				netip.MustParseAddr("192.0.2.30"),
			},
			wantStatus:     diagnostic.StatusPassed,
			wantAddressCnt: 1,
			wantIPv4:       []string{"192.0.2.30"},
			wantIPv6:       []string{},
		},
		{
			name: "duplicate addresses are removed",
			addresses: []netip.Addr{
				netip.MustParseAddr("192.0.2.40"),
				netip.MustParseAddr("192.0.2.40"),
			},
			wantStatus:     diagnostic.StatusPassed,
			wantAddressCnt: 1,
			wantIPv4:       []string{"192.0.2.40"},
			wantIPv6:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, addresses, err := ResolveDNS(
				context.Background(),
				fakeResolver{
					addresses: tt.addresses,
					err:       tt.resolverErr,
				},
				"example.test",
			)
			if err != nil {
				t.Fatalf("ResolveDNS() unexpected error: %v", err)
			}

			if result.Status != tt.wantStatus {
				t.Errorf(
					"status = %q, want %q",
					result.Status,
					tt.wantStatus,
				)
			}

			if result.Name != "dns" {
				t.Errorf("name = %q, want dns", result.Name)
			}

			if !result.Required {
				t.Error("DNS result should be required")
			}

			if err := result.Validate(); err != nil {
				t.Errorf("result.Validate() error: %v", err)
			}

			if len(addresses) != tt.wantAddressCnt {
				t.Errorf(
					"address count = %d, want %d",
					len(addresses),
					tt.wantAddressCnt,
				)
			}

			if tt.resolverErr != nil {
				if result.Error == "" {
					t.Error("expected resolver error to be preserved")
				}
				return
			}

			ipv4, ok := result.Details["ipv4_addresses"].([]string)
			if !ok {
				t.Fatalf(
					"ipv4_addresses has type %T, want []string",
					result.Details["ipv4_addresses"],
				)
			}

			ipv6, ok := result.Details["ipv6_addresses"].([]string)
			if !ok {
				t.Fatalf(
					"ipv6_addresses has type %T, want []string",
					result.Details["ipv6_addresses"],
				)
			}

			if !slices.Equal(ipv4, tt.wantIPv4) {
				t.Errorf("IPv4 addresses = %v, want %v", ipv4, tt.wantIPv4)
			}

			if !slices.Equal(ipv6, tt.wantIPv6) {
				t.Errorf("IPv6 addresses = %v, want %v", ipv6, tt.wantIPv6)
			}
		})
	}
}

func TestResolveDNSRejectsInvalidInput(t *testing.T) {
	resolver := fakeResolver{}

	tests := []struct {
		name     string
		ctx      context.Context
		resolver Resolver
		hostname string
	}{
		{
			name:     "nil context",
			resolver: resolver,
			hostname: "example.test",
		},
		{
			name:     "nil resolver",
			ctx:      context.Background(),
			hostname: "example.test",
		},
		{
			name:     "empty hostname",
			ctx:      context.Background(),
			resolver: resolver,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveDNS(
				tt.ctx,
				tt.resolver,
				tt.hostname,
			)
			if err == nil {
				t.Fatal("ResolveDNS() expected an error")
			}
		})
	}
}
