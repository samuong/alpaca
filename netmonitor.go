// Copyright 2019, 2021, 2024 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"log"
	"net"
	"slices"
)

type netMonitor interface {
	addrsChanged() bool
}

type netMonitorImpl struct {
	addrs    map[string]struct{}
	routes   []net.IP
	getAddrs func() ([]net.Addr, error)
	dial     func(network, addr string) (net.Conn, error)
}

func newNetMonitor() *netMonitorImpl {
	return &netMonitorImpl{getAddrs: net.InterfaceAddrs, dial: net.Dial}
}

func (nm *netMonitorImpl) addrsChanged() bool {
	addrs, err := nm.getAddrs()
	if err != nil {
		log.Printf("Error while getting network interface addresses: %q", err)
		return false
	}
	set := addrSliceToSet(addrs)
	// Probe for routes to a set of remote addresses. These addresses are
	// the same as those used by myIpAddressEx.
	// TODO: Cache the results so they don't need to be recalculated in
	// myIpAddress (and myIpAddressEx, when implemented).
	remotes := []string{
		"8.8.8.8", "2001:4860:4860::8888", // public addresses
		"10.0.0.0", "172.16.0.0", "192.168.0.0", "FC00::", // private addresses
	}
	locals := make([]net.IP, len(remotes))
	for i, remote := range remotes {
		locals[i] = nm.probeRoute(remote, false)
	}
	if setsAreEqual(set, nm.addrs) && slices.EqualFunc(locals, nm.routes, net.IP.Equal) {
		return false
	}
	nm.addrs = set
	nm.routes = locals
	return true
}

func addrSliceToSet(slice []net.Addr) map[string]struct{} {
	set := make(map[string]struct{})
	for _, addr := range slice {
		set[addr.String()] = struct{}{}
	}
	return set
}

func setsAreEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// probeRoute creates a UDP "connection" to the remote address, and returns the
// local interface address. This does involve a system call, but does not
// generate any network traffic since UDP is a connectionless protocol.
func (nm *netMonitorImpl) probeRoute(host string, ipv4only bool) net.IP {
	var network string
	if ipv4only {
		network = "udp4"
	} else {
		network = "udp"
	}
	conn, err := nm.dial(network, net.JoinHostPort(host, "80"))
	if err != nil {
		return nil
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		// Since we called dial with network set to "udp4" or "udp", we
		// expect this to be a *net.UDPAddr. If this fails, it's a bug
		// in Alpaca, and hopefully users will report it. But it's not
		// worth panicking over so we won't end the request here.
		log.Printf("unexpected: probeRoute host=%q ipv4only=%t: %v", host, ipv4only, err)
		return nil
	}
	if ip := local.IP; ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return nil
	}
	return local.IP
}
