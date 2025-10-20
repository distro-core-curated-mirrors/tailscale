//go:build darwin || windows

package netns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"tailscale.com/net/tsaddr"
	"tailscale.com/types/logger"
)

// tailscaleInterface returns the current machine's Tailscale interface, if any.
// If none is found, (nil, nil) is returned.
// A non-nil error is only returned on a problem listing the system interfaces.
func tailscaleInterface() (*net.Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifs {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				nip, ok := netip.AddrFromSlice(ipnet.IP)
				if ok && tsaddr.IsTailscaleIP(nip.Unmap()) {
					return &iface, nil
				}
			}
		}
	}
	return nil, nil
}

// inetReachability describes an interface and whether it was able to reach
// the provided address.
type inetReachability struct {
	iface     net.Interface
	reachable bool
	err       error
}

type probeOpts struct {
	logf    logger.Logf
	network string
	host    string
	port    string
	filterf interfaceFilter
	sortf   interfacePrioritySorter
}

type DefaultIfaceHintFn func() int

var defaultIfaceHintFn DefaultIfaceHintFn

// Platforms may set defaultIFQueryFn to a function that returns the platforms's high
// level view of the default interface index.
func SetDefaultIFQueryFn(fn DefaultIfaceHintFn) {
	defaultIfaceHintFn = fn
}

// uint
type bindFn func(c syscall.RawConn, ifidx uint32) error

// Returns the proper bind function for the given network and address.
// Currently only differentiates between IPv4 and IPv6 - and poorly.
func getBindFn(network, address string) bindFn {
	// Very naive check for IPv6.
	if strings.Contains(address, "]:") || strings.HasSuffix(network, "6") {
		return bindSocket6
	}
	return bindSocket4
}

// ProbeInterfacesReachability probes all non-loopback, up interfaces
// concurrently to determine which can reach the given address. It returns
// a slice with one entry per probed interface in the same order as
// net.Interfaces() filtered by the probe criteria.
func probeInterfacesReachability(opts probeOpts) ([]inetReachability, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		opts.logf("netns: ProbeInterfacesReachability: net.Interfaces: %v", err)
		return nil, err
	}

	// results channel sized to number of interfaces to avoid blocking.
	results := make(chan inetReachability, len(ifaces))

	tsiface, _ := tailscaleInterface()

	var candidates []net.Interface
	for _, iface := range ifaces {
		// Individual platforms can exclude potential intefaces based on platorm-specific logic.
		// For example, on Darwin, we skip "utun" interfaces.
		if opts.filterf != nil && !opts.filterf(iface) {
			continue
		}

		// Only consider up, non-loopback interfaces.
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagRunning == 0 {
			continue
		}

		// Skip the Tailscale interface.
		if tsiface != nil && iface.Index == tsiface.Index {
			continue
		}

		// require an IPv4 or IPv6 global unicast address
		if !ifaceHasV4OrGlobalV6(&iface) {
			//logf("netns: ProbeInterfacesReachability: skipping %q: missing v4 or global v6", iface.Name)
			continue
		}

		candidates = append(candidates, iface)
	}

	// (barnstar) TODO: The original idea here had us racing to find the best match but that will not
	// be the correct approach on platforms which may have multiple active interfaces (e.g. cellular + wifi).
	// We need to probe all interfaces and return the results to the caller so they can make the
	// decision about which interface to use.  For now, we will just probe them all.
	for _, iface := range candidates {
		go func() {
			// Per-probe timeout.
			dialCtx, dialCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer dialCancel()

			// (barnstar) TODO: Do we need to do the full dail or can we just bind here and
			// check for errors?
			d := net.Dialer{
				Control: func(network, address string, c syscall.RawConn) error {
					// (barnstar) TODO: The bind step here is still platform specific
					bindFn := getBindFn(network, address)
					return bindFn(c, uint32(iface.Index))
				},
			}

			dst := net.JoinHostPort(opts.host, opts.port)

			conn, err := d.DialContext(dialCtx, opts.network, dst)

			if err != nil {
				results <- inetReachability{iface: iface, reachable: false, err: err}
				return
			}
			defer conn.Close()
			results <- inetReachability{iface: iface, reachable: true, err: nil}
		}()
	}

	if len(candidates) == 0 {
		opts.logf("netns: ProbeInterfacesReachability: no candidate interfaces found")
		return nil, errors.New("no candidate interfaces")
	}

	out := make([]inetReachability, 0, len(candidates))
	timeout := time.After(600 * time.Millisecond)
	received := 0
	for received < len(candidates) {
		select {
		case r := <-results:
			out = append(out, r)
			received++
		case <-timeout:
			return out, fmt.Errorf("netns: probe timed out after %v; received %d/%d results", 700*time.Millisecond, received, len(candidates))
		}
	}

	return out, nil
}

// Pre-filter for interfaces.  Platform-specific code can provide a filter
// to exclude certain interfaces from consideration.  For example, on Darwin,
// we exclude "utun" interfaces.
type interfaceFilter func(net.Interface) bool

// Takes a list of interfaces and returns a sorted list according to
// their priority.  Platform-specific code can provide a sorter to
// prioritize certain interfaces over others.  For example, on Darwin,
// we can prioritize "en0" (Wi-Fi) over "pdp_ip0" (cellular).
//
// (barnstar) TODO: This may be tricky to implement in pure go.  For example,
// BSD names are really not API, and "expensive" is not a concept we can get
// without querying some higher level API.  We may need to plumb in some cgo
// here.   Also, an interface that is "failing" may need to be deprioritized,
// and we have no way of knowing that here.
//
// The system's default route interface should probably be highest priority, but
// now we're going around in circles...  Perhaps a "hint" system that the
// caller can provide to indicate which interface to prioritize or to deprioritize.
//
// If netmon *knows* the default route interface, perhaps it can set a priority
// flag for that idx.  If a Dial fails, perhaps it can set a "failed recently" flag
type interfacePrioritySorter func([]inetReachability) []inetReachability

func filterInPlace[T any](s []T, keep func(T) bool) []T {
	i := 0
	for _, v := range s {
		if keep(v) {
			s[i] = v
			i++
		}
	}
	return s[:i]
}

var errUnspecifiedHost = errors.New("unspecified host")

func parseAddress(address string) (addr netip.Addr, err error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// error means the string didn't contain a port number, so use the string directly
		host = address
	}
	if host == "" {
		return addr, errUnspecifiedHost
	}

	return netip.ParseAddr(host)
}

// findInterfaceThatCanReach finds an interface that can reach the given host:port.
// It uses the provided filterf to exclude certain interfaces, and the
// sortf to prioritize certain interfaces. It returns the first interface that can reach
// the destination.
//
// nil is returned if no interface can reach the destination.
func findInterfaceThatCanReach(opts probeOpts) (iface *net.Interface, err error) {
	res, err := probeInterfacesReachability(opts)
	if err != nil {
		opts.logf("netns: ProbeInterfacesReachability error: %v", err)
		return nil, err
	}

	// The filtering and sorting here ranges over a small handful of interfaces,
	// typically one or two.
	res = filterInPlace(res, func(r inetReachability) bool { return r.reachable })
	if len(res) == 0 {
		opts.logf("netns: could not find interface on network %v to reach %q:%q on %q: %v", opts.network, opts.host, opts.port, opts.network, err)
		return nil, nil
	}

	if opts.sortf != nil {
		res = opts.sortf(res)
	}

	candidatesNames := make([]string, 0, len(res))
	for _, r := range res {
		candidatesNames = append(candidatesNames, r.iface.Name)
	}
	opts.logf("netns: found candidate interfaces that can reach %v:%v on %v:  %v", opts.host, opts.port, opts.network, candidatesNames)

	// (barstar) TODO: If we have multiple interfaces that can *potentially* reach
	// the destination, how to we get back here to pick the "next best" interface
	//
	// Think hostile wifi.  We can bind to the interface, but further down the line,
	// we'll have a bad time - can we fall back to ethernet, or cellular, or whatever?
	// And how do we revert to the less expensive interface when the hostile wifi becomes
	// "less hostile"?   Is the read/write probe a above good enough?
	iface = &res[0].iface

	if defaultIfaceHintFn != nil {
		defIdx := defaultIfaceHintFn()
		for _, r := range res {
			if r.iface.Index == defIdx {
				opts.logf("netns: using default iface hint")
				iface = &r.iface
				break
			}
		}
	}

	opts.logf("netns: returning interface %v at %v for %v:%v", iface.Name, iface.Index, opts.host, opts.port)
	return iface, nil
}

// ifaceHasV4AndGlobalV6 reports whether iface has at least one IPv4 address
// and at least one IPv6 address that is not link-local.
func ifaceHasV4OrGlobalV6(iface *net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		switch v := a.(type) {
		case *net.IPNet:
			if v.IP.IsGlobalUnicast() {
				return true
			}

		}
	}
	return false
}
