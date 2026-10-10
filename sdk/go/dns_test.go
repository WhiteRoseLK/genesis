// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"testing"
)

func TestFQDN(t *testing.T) {
	if got := FQDN("example.com"); got != "example.com." {
		t.Errorf("FQDN(example.com) = %q, want example.com.", got)
	}
	if got := FQDN("example.com."); got != "example.com." {
		t.Errorf("FQDN(example.com.) = %q, want example.com.", got)
	}
}

func TestRecordFQDN(t *testing.T) {
	if got := RecordFQDN("example.com", "@"); got != "example.com." {
		t.Errorf("RecordFQDN(example.com, @) = %q, want example.com.", got)
	}
	if got := RecordFQDN("example.com", ""); got != "example.com." {
		t.Errorf("RecordFQDN(example.com, '') = %q, want example.com.", got)
	}
	if got := RecordFQDN("example.com", "api"); got != "api.example.com." {
		t.Errorf("RecordFQDN(example.com, api) = %q, want api.example.com.", got)
	}
}

func TestReversePTRForA(t *testing.T) {
	t.Run("valid IPv4", func(t *testing.T) {
		rec, err := ReversePTRForA("10.255.255.1", "verify-probe.lab.internal", 300)
		if err != nil {
			t.Fatalf("ReversePTRForA: %v", err)
		}
		if rec.GetZone() != "255.255.10.in-addr.arpa" {
			t.Errorf("Zone = %q, want 255.255.10.in-addr.arpa", rec.GetZone())
		}
		if rec.GetName() != "1" {
			t.Errorf("Name = %q, want 1", rec.GetName())
		}
		if rec.GetType() != "PTR" {
			t.Errorf("Type = %q, want PTR", rec.GetType())
		}
		if len(rec.GetValues()) != 1 || rec.GetValues()[0] != "verify-probe.lab.internal." {
			t.Errorf("Values = %v, want [verify-probe.lab.internal.]", rec.GetValues())
		}
		if rec.GetTtl() != 300 {
			t.Errorf("Ttl = %d, want 300", rec.GetTtl())
		}
	})

	t.Run("default TTL", func(t *testing.T) {
		rec, err := ReversePTRForA("10.0.0.1", "host.domain.", 0)
		if err != nil {
			t.Fatalf("ReversePTRForA: %v", err)
		}
		if rec.GetTtl() != 300 {
			t.Errorf("Ttl = %d, want 300", rec.GetTtl())
		}
	})

	t.Run("invalid IP", func(t *testing.T) {
		_, err := ReversePTRForA("invalid-ip", "example.com", 300)
		if err == nil {
			t.Error("expected error for invalid IP")
		}
	})

	t.Run("IPv6 not supported for ReversePTRForA", func(t *testing.T) {
		_, err := ReversePTRForA("::1", "example.com", 300)
		if err == nil {
			t.Error("expected error for IPv6")
		}
	})
}

func TestReverseKeyForA(t *testing.T) {
	key, err := ReverseKeyForA("192.168.1.50")
	if err != nil {
		t.Fatalf("ReverseKeyForA: %v", err)
	}
	if key.GetZone() != "1.168.192.in-addr.arpa" {
		t.Errorf("Zone = %q, want 1.168.192.in-addr.arpa", key.GetZone())
	}
	if key.GetName() != "50" {
		t.Errorf("Name = %q, want 50", key.GetName())
	}
	if key.GetType() != "PTR" {
		t.Errorf("Type = %q, want PTR", key.GetType())
	}

	_, err = ReverseKeyForA("invalid")
	if err == nil {
		t.Error("expected error for invalid IP")
	}

	_, err = ReverseKeyForA("::1")
	if err == nil {
		t.Error("expected error for IPv6")
	}
}
