package main

import (
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

type mockAddr string

func (a mockAddr) Network() string {
	return "ip+net"
}

func (a mockAddr) String() string {
	return string(a)
}

func toAddrs(ss ...string) []net.Addr {
	addrs := make([]net.Addr, len(ss))
	for i, s := range ss {
		addrs[i] = mockAddr(s)
	}
	return addrs
}

func TestNetworkMonitor(t *testing.T) {
	var next []net.Addr
	nm := NetMonitor{make(map[string]struct{}), func() ([]net.Addr, error) { return next, nil }}
	// Start with just loopback interfaces
	next = toAddrs("127.0.0.1/8", "::1/128")
	assert.True(t, nm.AddrsChanged())
	// Connect to network, and get local IPv4 and IPv6 addresses
	next = toAddrs("127.0.0.1/8", "192.168.1.6/24", "::1/128", "fe80::dfd9:fe1d:56d1:1f3a/64")
	assert.True(t, nm.AddrsChanged())
	// Stay connected, nothing changed
	next = toAddrs("127.0.0.1/8", "192.168.1.6/24", "::1/128", "fe80::dfd9:fe1d:56d1:1f3a/64")
	assert.False(t, nm.AddrsChanged())
	// DHCP lease expires, get new addresses
	next = toAddrs("127.0.0.1/8", "192.168.1.7/24", "::1/128", "fe80::dfd9:fe1d:56d1:1f3b/64")
	assert.True(t, nm.AddrsChanged())
	// Disconnect, and go back to having just loopback addresses
	next = toAddrs("127.0.0.1/8", "::1/128")
	assert.True(t, nm.AddrsChanged())
}
