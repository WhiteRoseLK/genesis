// SPDX-License-Identifier: Apache-2.0

package ipam

import (
	"errors"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/spec"
)

func TestParsePool(t *testing.T) {
	tests := []struct {
		name      string
		pool      string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "range syntax",
			pool:      "10.10.0.50-10.10.0.99",
			wantCount: 50,
		},
		{
			name:      "range syntax with spaces",
			pool:      " 10.10.0.50 - 10.10.0.99 ",
			wantCount: 50,
		},
		{
			name:      "shorthand range syntax",
			pool:      "10.10.0.50-99",
			wantCount: 50,
		},
		{
			name:      "cidr syntax /24",
			pool:      "10.10.0.0/24",
			wantCount: 254, // excludes .0 and .255
		},
		{
			name:      "cidr syntax /30",
			pool:      "10.10.0.0/30",
			wantCount: 2, // .1 and .2
		},
		{
			name:      "cidr syntax /31",
			pool:      "10.10.0.0/31",
			wantCount: 2, // .0 and .1 (RFC 3021)
		},
		{
			name:      "cidr syntax /32",
			pool:      "10.10.0.5/32",
			wantCount: 1,
		},
		{
			name:      "single ip",
			pool:      "10.10.0.50",
			wantCount: 1,
		},
		{
			name:      "comma-separated combination with overlap merging",
			pool:      "10.10.0.10-10.10.0.20, 10.10.0.15-10.10.0.30, 10.10.0.40",
			wantCount: 22, // 10-30 (21 IPs) + 40 (1 IP)
		},
		{
			name:    "empty string",
			pool:    "",
			wantErr: true,
		},
		{
			name:    "invalid start IP",
			pool:    "invalid-10.10.0.10",
			wantErr: true,
		},
		{
			name:    "invalid end IP",
			pool:    "10.10.0.10-invalid",
			wantErr: true,
		},
		{
			name:    "start greater than end",
			pool:    "10.10.0.100-10.10.0.50",
			wantErr: true,
		},
		{
			name:    "invalid cidr",
			pool:    "10.10.0.0/33",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranges, err := ParsePool(tc.pool)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParsePool(%q) expected error, got nil", tc.pool)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePool(%q) unexpected error: %v", tc.pool, err)
			}
			count := 0
			for _, r := range ranges {
				count += int(r.End - r.Start + 1)
			}
			if count != tc.wantCount {
				t.Errorf("ParsePool(%q) capacity = %d, want %d", tc.pool, count, tc.wantCount)
			}
		})
	}
}

func TestAllocateSequentialAndIdempotent(t *testing.T) {
	alloc, err := New("10.10.0.50-10.10.0.53", Options{
		Gateway: "10.10.0.1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if alloc.Gateway() != "10.10.0.1" {
		t.Errorf("Gateway() = %q, want 10.10.0.1", alloc.Gateway())
	}

	ip1, err := alloc.Allocate("vm1")
	if err != nil {
		t.Fatalf("Allocate(vm1): %v", err)
	}
	if ip1 != "10.10.0.50" {
		t.Errorf("Allocate(vm1) = %q, want 10.10.0.50", ip1)
	}

	// Idempotent: same VM returns same IP
	ip1Again, err := alloc.Allocate("vm1")
	if err != nil {
		t.Fatalf("Allocate(vm1) again: %v", err)
	}
	if ip1Again != ip1 {
		t.Errorf("Allocate(vm1) again = %q, want %q", ip1Again, ip1)
	}

	ip2, err := alloc.Allocate("vm2")
	if err != nil {
		t.Fatalf("Allocate(vm2): %v", err)
	}
	if ip2 != "10.10.0.51" {
		t.Errorf("Allocate(vm2) = %q, want 10.10.0.51", ip2)
	}

	ip3, err := alloc.Allocate("vm3")
	if err != nil {
		t.Fatalf("Allocate(vm3): %v", err)
	}
	if ip3 != "10.10.0.52" {
		t.Errorf("Allocate(vm3) = %q, want 10.10.0.52", ip3)
	}

	ip4, err := alloc.Allocate("vm4")
	if err != nil {
		t.Fatalf("Allocate(vm4): %v", err)
	}
	if ip4 != "10.10.0.53" {
		t.Errorf("Allocate(vm4) = %q, want 10.10.0.53", ip4)
	}

	// Pool exhausted
	_, err = alloc.Allocate("vm5")
	if !errors.Is(err, ErrPoolExhausted) {
		t.Fatalf("Allocate(vm5) error = %v, want ErrPoolExhausted", err)
	}

	// Release vm2 -> 10.10.0.51 freed
	if err := alloc.Release("vm2"); err != nil {
		t.Fatalf("Release(vm2): %v", err)
	}

	// vm5 can now take 10.10.0.51
	ip5, err := alloc.Allocate("vm5")
	if err != nil {
		t.Fatalf("Allocate(vm5) after release: %v", err)
	}
	if ip5 != "10.10.0.51" {
		t.Errorf("Allocate(vm5) = %q, want 10.10.0.51", ip5)
	}
}

func TestReservedAddressesSkipped(t *testing.T) {
	// Pool includes 10.10.0.50 to 10.10.0.53, but 10.10.0.50 and 10.10.0.52 are reserved.
	alloc, err := New("10.10.0.50-10.10.0.53", Options{
		Gateway:  "10.10.0.50",
		Reserved: []string{"10.10.0.52"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ip1, err := alloc.Allocate("vm1")
	if err != nil {
		t.Fatalf("Allocate(vm1): %v", err)
	}
	if ip1 != "10.10.0.51" {
		t.Errorf("Allocate(vm1) = %q, want 10.10.0.51 (skipped gateway 10.10.0.50)", ip1)
	}

	ip2, err := alloc.Allocate("vm2")
	if err != nil {
		t.Fatalf("Allocate(vm2): %v", err)
	}
	if ip2 != "10.10.0.53" {
		t.Errorf("Allocate(vm2) = %q, want 10.10.0.53 (skipped reserved 10.10.0.52)", ip2)
	}
}

func TestPreAllocatedStateAndPersistence(t *testing.T) {
	var persisted map[string]string
	onChange := func(allocated map[string]string) error {
		persisted = allocated
		return nil
	}

	alloc, err := New("10.10.0.50-10.10.0.60", Options{
		Allocated: map[string]string{
			"existing1": "10.10.0.50",
			"existing2": "10.10.0.52",
		},
		OnChange: onChange,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Existing VM lookup
	ip, ok := alloc.Get("existing1")
	if !ok || ip != "10.10.0.50" {
		t.Errorf("Get(existing1) = (%q, %v), want (10.10.0.50, true)", ip, ok)
	}

	// New allocation skips .50 and takes .51
	ipNew, err := alloc.Allocate("new_vm")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if ipNew != "10.10.0.51" {
		t.Errorf("Allocate(new_vm) = %q, want 10.10.0.51", ipNew)
	}

	if persisted["new_vm"] != "10.10.0.51" {
		t.Errorf("OnChange did not persist new_vm: %+v", persisted)
	}
}

func TestPersistenceErrorRollback(t *testing.T) {
	failPersist := true
	onChange := func(_ map[string]string) error {
		if failPersist {
			return errors.New("disk full")
		}
		return nil
	}

	alloc, err := New("10.10.0.50-10.10.0.60", Options{
		OnChange: onChange,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = alloc.Allocate("vm1")
	if err == nil {
		t.Fatal("expected allocate to fail when OnChange returns error")
	}

	// Verify allocation was rolled back
	if _, ok := alloc.Get("vm1"); ok {
		t.Error("vm1 should not be allocated after rollback")
	}

	// Allow persist and try again
	failPersist = false
	ip, err := alloc.Allocate("vm1")
	if err != nil {
		t.Fatalf("Allocate(vm1) after fix: %v", err)
	}
	if ip != "10.10.0.50" {
		t.Errorf("Allocate(vm1) = %q, want 10.10.0.50", ip)
	}
}

func TestValidateNetwork(t *testing.T) {
	validNet := spec.Network{
		CIDR:    "10.10.0.0/24",
		Gateway: "10.10.0.1",
		Pool:    "10.10.0.50-10.10.0.99",
	}
	validSeed := spec.Seed{
		Address: "10.10.0.10",
	}

	if err := ValidateNetwork(validNet, validSeed); err != nil {
		t.Errorf("ValidateNetwork(valid) = %v, want nil", err)
	}

	// Gateway inside pool
	badGwNet := validNet
	badGwNet.Gateway = "10.10.0.60"
	if err := ValidateNetwork(badGwNet, validSeed); err == nil {
		t.Error("ValidateNetwork should fail when gateway is inside pool")
	}

	// Seed inside pool
	badSeed := validSeed
	badSeed.Address = "10.10.0.70"
	if err := ValidateNetwork(validNet, badSeed); err == nil {
		t.Error("ValidateNetwork should fail when seed is inside pool")
	}

	// Gateway outside CIDR
	outGwNet := validNet
	outGwNet.Gateway = "192.168.1.1"
	if err := ValidateNetwork(outGwNet, validSeed); err == nil {
		t.Error("ValidateNetwork should fail when gateway is outside CIDR")
	}

	// Pool outside CIDR
	outPoolNet := validNet
	outPoolNet.Pool = "192.168.1.50-192.168.1.99"
	if err := ValidateNetwork(outPoolNet, validSeed); err == nil {
		t.Error("ValidateNetwork should fail when pool is outside CIDR")
	}
}
