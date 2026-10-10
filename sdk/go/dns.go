// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"fmt"
	"net"
	"strings"

	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
)

// FQDN ensures the domain name ends with a trailing dot.
func FQDN(s string) string {
	if strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

// RecordFQDN returns the fully-qualified domain name for a record in a zone.
func RecordFQDN(zone, name string) string {
	if name == "" || name == "@" {
		return FQDN(zone)
	}
	return FQDN(name + "." + zone)
}

// ReversePTRForA derives the matching PTR record and in-addr.arpa zone for an
// IPv4 address and target FQDN.
func ReversePTRForA(ip, fqdn string, ttl uint32) (*dnszonev1.Record, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, fmt.Errorf("invalid IP address: %q", ip)
	}
	ipv4 := parsed.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("IP address %q is not IPv4", ip)
	}
	zone := fmt.Sprintf("%d.%d.%d.in-addr.arpa", ipv4[2], ipv4[1], ipv4[0])
	name := fmt.Sprintf("%d", ipv4[3])
	if !strings.HasSuffix(fqdn, ".") {
		fqdn += "."
	}
	if ttl == 0 {
		ttl = 300
	}
	return &dnszonev1.Record{
		Zone:   zone,
		Name:   name,
		Type:   "PTR",
		Values: []string{fqdn},
		Ttl:    ttl,
	}, nil
}

// ReverseKeyForA returns the reverse record key (zone, name, type) for an IPv4 address.
func ReverseKeyForA(ip string) (*dnszonev1.RecordKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, fmt.Errorf("invalid IP address: %q", ip)
	}
	ipv4 := parsed.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("IP address %q is not IPv4", ip)
	}
	zone := fmt.Sprintf("%d.%d.%d.in-addr.arpa", ipv4[2], ipv4[1], ipv4[0])
	name := fmt.Sprintf("%d", ipv4[3])
	return &dnszonev1.RecordKey{
		Zone: zone,
		Name: name,
		Type: "PTR",
	}, nil
}
