// SPDX-License-Identifier: Apache-2.0

// Package ipam manages IPv4 address allocation from the network pool
// defined in the environment spec (docs/04-spec.md).
package ipam

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"

	"github.com/WhiteRoseLK/genesis/internal/spec"
)

var (
	// ErrPoolExhausted reports that no free IP address remains in the pool.
	ErrPoolExhausted = errors.New("ipam: pool exhausted")
	// ErrInvalidPool reports that the pool definition syntax is invalid.
	ErrInvalidPool = errors.New("ipam: invalid pool definition")
)

// Options configures an Allocator.
type Options struct {
	// Gateway is the network gateway IP, reserved from allocation.
	Gateway string
	// Reserved is a list of IP addresses that must not be allocated.
	Reserved []string
	// Allocated contains pre-existing VM name -> IP allocations.
	Allocated map[string]string
	// OnChange is an optional callback triggered whenever an allocation is made or released.
	OnChange func(allocated map[string]string) error
}

// IPRange represents an inclusive range of IPv4 addresses.
type IPRange struct {
	Start uint32
	End   uint32
}

// Allocator assigns IP addresses sequentially from defined IP ranges.
type Allocator struct {
	mu        sync.Mutex
	ranges    []IPRange
	gateway   string
	reserved  map[uint32]bool
	allocated map[string]uint32
	usedIPs   map[uint32]string
	onChange  func(allocated map[string]string) error
}

// New creates an Allocator for the given pool string.
func New(poolStr string, opts Options) (*Allocator, error) {
	ranges, err := ParsePool(poolStr)
	if err != nil {
		return nil, err
	}

	alloc := &Allocator{
		ranges:    ranges,
		gateway:   opts.Gateway,
		reserved:  make(map[uint32]bool),
		allocated: make(map[string]uint32),
		usedIPs:   make(map[uint32]string),
		onChange:  opts.OnChange,
	}

	if opts.Gateway != "" {
		if gwAddr, err := netip.ParseAddr(opts.Gateway); err == nil && gwAddr.Is4() {
			alloc.reserved[ipToUint32(gwAddr)] = true
		}
	}

	for _, resStr := range opts.Reserved {
		if resStr == "" {
			continue
		}
		if resAddr, err := netip.ParseAddr(resStr); err == nil && resAddr.Is4() {
			alloc.reserved[ipToUint32(resAddr)] = true
		}
	}

	for name, ipStr := range opts.Allocated {
		addr, err := netip.ParseAddr(ipStr)
		if err != nil || !addr.Is4() {
			continue
		}
		val := ipToUint32(addr)
		alloc.allocated[name] = val
		alloc.usedIPs[val] = name
	}

	return alloc, nil
}

// Gateway returns the configured network gateway address.
func (a *Allocator) Gateway() string {
	return a.gateway
}

// Allocate allocates an IP address for VM name, or returns the existing IP if already allocated.
func (a *Allocator) Allocate(name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if val, ok := a.allocated[name]; ok {
		return uint32ToIP(val).String(), nil
	}

	for _, r := range a.ranges {
		for val := r.Start; val <= r.End; val++ {
			if a.reserved[val] {
				continue
			}
			if _, used := a.usedIPs[val]; used {
				continue
			}

			a.allocated[name] = val
			a.usedIPs[val] = name

			if a.onChange != nil {
				if err := a.onChange(a.exportAllocated()); err != nil {
					delete(a.allocated, name)
					delete(a.usedIPs, val)
					return "", fmt.Errorf("ipam: persisting allocation for %q: %w", name, err)
				}
			}

			return uint32ToIP(val).String(), nil
		}
	}

	return "", fmt.Errorf("%w: no free IP address for VM %q", ErrPoolExhausted, name)
}

// Release frees the IP address associated with VM name.
func (a *Allocator) Release(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	val, ok := a.allocated[name]
	if !ok {
		return nil
	}

	delete(a.allocated, name)
	delete(a.usedIPs, val)

	if a.onChange != nil {
		if err := a.onChange(a.exportAllocated()); err != nil {
			a.allocated[name] = val
			a.usedIPs[val] = name
			return fmt.Errorf("ipam: persisting release for %q: %w", name, err)
		}
	}

	return nil
}

// Get returns the allocated IP for name if present.
func (a *Allocator) Get(name string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	val, ok := a.allocated[name]
	if !ok {
		return "", false
	}
	return uint32ToIP(val).String(), true
}

// Allocated returns a copy of all current name -> IP allocations.
func (a *Allocator) Allocated() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.exportAllocated()
}

func (a *Allocator) exportAllocated() map[string]string {
	res := make(map[string]string, len(a.allocated))
	for name, val := range a.allocated {
		res[name] = uint32ToIP(val).String()
	}
	return res
}

// Contains reports whether ip is within any range of the pool.
func (a *Allocator) Contains(ip netip.Addr) bool {
	if !ip.Is4() {
		return false
	}
	val := ipToUint32(ip)
	for _, r := range a.ranges {
		if val >= r.Start && val <= r.End {
			return true
		}
	}
	return false
}

// TotalCapacity returns the total count of IPs within the pool.
func (a *Allocator) TotalCapacity() int {
	total := 0
	for _, r := range a.ranges {
		total += int(r.End - r.Start + 1)
	}
	return total
}

// ParsePool parses a pool specification into merged, normalized IPv4 ranges.
// Supported syntax:
//   - Range: "10.10.0.50-10.10.0.99" or shorthand "10.10.0.50-99"
//   - CIDR: "10.10.0.0/24" (excludes network and broadcast for /1-/30)
//   - Single IP: "10.10.0.50"
//   - Comma-separated combination of the above
func ParsePool(poolStr string) ([]IPRange, error) {
	poolStr = strings.TrimSpace(poolStr)
	if poolStr == "" {
		return nil, fmt.Errorf("%w: empty pool string", ErrInvalidPool)
	}

	parts := strings.Split(poolStr, ",")
	var rawRanges []IPRange

	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "-") {
			dashParts := strings.Split(entry, "-")
			if len(dashParts) != 2 {
				return nil, fmt.Errorf("%w: invalid range %q", ErrInvalidPool, entry)
			}
			startStr := strings.TrimSpace(dashParts[0])
			endStr := strings.TrimSpace(dashParts[1])

			startAddr, err := netip.ParseAddr(startStr)
			if err != nil || !startAddr.Is4() {
				return nil, fmt.Errorf("%w: invalid start IP %q in range %q", ErrInvalidPool, startStr, entry)
			}

			// Handle shorthand format like 10.10.0.50-99
			if !strings.Contains(endStr, ".") {
				lastDot := strings.LastIndex(startStr, ".")
				if lastDot == -1 {
					return nil, fmt.Errorf("%w: invalid range %q", ErrInvalidPool, entry)
				}
				endStr = startStr[:lastDot+1] + endStr
			}

			endAddr, err := netip.ParseAddr(endStr)
			if err != nil || !endAddr.Is4() {
				return nil, fmt.Errorf("%w: invalid end IP %q in range %q", ErrInvalidPool, endStr, entry)
			}

			startVal := ipToUint32(startAddr)
			endVal := ipToUint32(endAddr)
			if startVal > endVal {
				return nil, fmt.Errorf("%w: start IP %s is greater than end IP %s", ErrInvalidPool, startAddr, endAddr)
			}

			rawRanges = append(rawRanges, IPRange{Start: startVal, End: endVal})
			continue
		}

		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("%w: invalid CIDR %q", ErrInvalidPool, entry)
			}

			startAddr := prefix.Masked().Addr()
			start := ipToUint32(startAddr)
			mask := prefix.Bits()
			hostCount := uint32(1) << (32 - mask)
			broadcast := start + hostCount - 1

			var rStart, rEnd uint32
			if mask <= 30 {
				rStart = start + 1
				rEnd = broadcast - 1
			} else if mask == 31 {
				rStart = start
				rEnd = broadcast
			} else {
				rStart = start
				rEnd = start
			}

			rawRanges = append(rawRanges, IPRange{Start: rStart, End: rEnd})
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil || !addr.Is4() {
			return nil, fmt.Errorf("%w: invalid IP address %q", ErrInvalidPool, entry)
		}
		val := ipToUint32(addr)
		rawRanges = append(rawRanges, IPRange{Start: val, End: val})
	}

	if len(rawRanges) == 0 {
		return nil, fmt.Errorf("%w: no valid ranges found in %q", ErrInvalidPool, poolStr)
	}

	sort.Slice(rawRanges, func(i, j int) bool {
		return rawRanges[i].Start < rawRanges[j].Start
	})

	var merged []IPRange
	for _, r := range rawRanges {
		if len(merged) == 0 {
			merged = append(merged, r)
			continue
		}
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 {
			if r.End > last.End {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}

	return merged, nil
}

// ValidateNetwork verifies network boundaries as specified in docs/04-spec.md
// and docs/user-guide.md:
//   - network.cidr must be a valid IPv4 CIDR
//   - network.gateway and seed.address must be valid IPs within network.cidr
//   - network.pool must be valid and entirely within network.cidr
//   - network.gateway and seed.address must NOT fall within network.pool
func ValidateNetwork(netCfg spec.Network, seedCfg spec.Seed) error {
	cidrPrefix, err := netip.ParsePrefix(netCfg.CIDR)
	if err != nil || !cidrPrefix.Addr().Is4() {
		return fmt.Errorf("invalid network.cidr %q: must be a valid IPv4 prefix", netCfg.CIDR)
	}

	gwAddr, err := netip.ParseAddr(netCfg.Gateway)
	if err != nil || !gwAddr.Is4() {
		return fmt.Errorf("invalid network.gateway %q: must be a valid IPv4 address", netCfg.Gateway)
	}
	if !cidrPrefix.Contains(gwAddr) {
		return fmt.Errorf("network.gateway %s is outside network.cidr %s", gwAddr, cidrPrefix)
	}

	if seedCfg.Address != "" {
		seedAddr, err := netip.ParseAddr(seedCfg.Address)
		if err != nil || !seedAddr.Is4() {
			return fmt.Errorf("invalid seed.address %q: must be a valid IPv4 address", seedCfg.Address)
		}
		if !cidrPrefix.Contains(seedAddr) {
			return fmt.Errorf("seed.address %s is outside network.cidr %s", seedAddr, cidrPrefix)
		}
	}

	if netCfg.Pool == "" {
		return errors.New("network.pool is required")
	}

	ranges, err := ParsePool(netCfg.Pool)
	if err != nil {
		return fmt.Errorf("invalid network.pool %q: %w", netCfg.Pool, err)
	}

	for _, r := range ranges {
		startAddr := uint32ToIP(r.Start)
		endAddr := uint32ToIP(r.End)
		if !cidrPrefix.Contains(startAddr) || !cidrPrefix.Contains(endAddr) {
			return fmt.Errorf("network.pool range %s-%s is outside network.cidr %s", startAddr, endAddr, cidrPrefix)
		}
	}

	if containsVal(ranges, ipToUint32(gwAddr)) {
		return fmt.Errorf("network.gateway %s is inside the allocatable pool %s", gwAddr, netCfg.Pool)
	}

	if seedCfg.Address != "" {
		seedAddr, _ := netip.ParseAddr(seedCfg.Address)
		if containsVal(ranges, ipToUint32(seedAddr)) {
			return fmt.Errorf("seed.address %s is inside the allocatable pool %s", seedAddr, netCfg.Pool)
		}
	}

	return nil
}

func containsVal(ranges []IPRange, val uint32) bool {
	for _, r := range ranges {
		if val >= r.Start && val <= r.End {
			return true
		}
	}
	return false
}

func ipToUint32(ip netip.Addr) uint32 {
	b := ip.As4()
	return binary.BigEndian.Uint32(b[:])
}

func uint32ToIP(n uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return netip.AddrFrom4(b)
}
