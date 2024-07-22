// Copyright 2019, 2024 The Alpaca Authors
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
	"errors"
	"math/rand/v2"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// In order to test netMonitor, we use mock implementations of the
// net.InterfaceAddrs() and net.Dial() functions, as well as the net.Addr and
// net.Conn types. The mocks below will implement just enough functionality to
// allow the tests to run, and will panic if unimplemented functions are
// called. We simulate three network states: "offline", "wifi" and "vpn".
//
// In "offline" mode, only the loopback addresses exist, and attempts to dial
// anywhere will result in a "network is unreachable" error.
//
// In "wifi" mode, in addition to the loopback addresses, we've also got an IP
// address in the 192.168.1.0/24 range, which is meant to look like a home wifi
// router, and we simulate a routing table that routes everything through this
// interface.
//
// In "vpn" mode, we've got the same IP addresses as in "wifi" mode, and any
// connection attempts will be routed via an address in the 10.0.0.0/8 range
// (this is meant to look like a private corporate network). Note that our
// 10.0.0.0/8 address does *not* appear in the output of net.InterfaceAddrs()
// because apparently some VPN clients behave like this.

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

type mockConn struct {
	localAddr net.Addr
}

var _ net.Conn = mockConn{}

func (c mockConn) Close() error {
	return nil
}

func (c mockConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c mockConn) Read(b []byte) (n int, err error) {
	panic("unreachable")
}

func (c mockConn) RemoteAddr() net.Addr {
	panic("unreachable")
}

func (c mockConn) SetDeadline(t time.Time) error {
	panic("unreachable")
}

func (c mockConn) SetReadDeadline(t time.Time) error {
	panic("unreachable")
}

func (c mockConn) SetWriteDeadline(t time.Time) error {
	panic("unreachable")
}

func (c mockConn) Write(b []byte) (n int, err error) {
	panic("unreachable")
}

type mockNet struct {
	state string
}

func (n *mockNet) interfaceAddrs() ([]net.Addr, error) {
	var addrs []net.Addr
	switch n.state {
	case "vpn", "wifi":
		addrs = append(addrs, toAddrs("192.168.1.2/24", "fe80::fedc:ba98:7654:3210/64")...)
		fallthrough
	case "offline":
		addrs = append(addrs, toAddrs("127.0.0.1/8", "::1/128")...)
	default:
		panic("interfaceAddrs state=" + n.state)
	}
	return addrs, nil
}

func (n *mockNet) dial(network, address string) (net.Conn, error) {
	if network != "udp" && network != "udp4" {
		panic("dial network=" + network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		panic("dial: " + err.Error())
	}
	ip := net.ParseIP(host)
	if ip == nil {
		panic("dial host=" + host)
	}
	if ipv4 := ip.To4(); ipv4 == nil {
		// Pretend we can't route to any IPv6 addresses.
		return nil, newDialError(network, address, "connect: no route to host")
	}
	switch n.state {
	case "vpn":
		// Pretend we're routing through a corporate VPN.
		return newMockConn(10, 0, 0, 3), nil
	case "wifi":
		// Pretend we're routing through a home WiFi router.
		return newMockConn(192, 168, 1, 2), nil
	case "offline":
		// Pretend we can't route to anywhere.
		return nil, newDialError(network, address, "connect: network is unreachable")
	default:
		panic("dial state=" + n.state)
	}
}

func newMockConn(a, b, c, d byte) mockConn {
	// Pretend that the operating system has assigned a random port (in the
	// range 1024 to 65535) on the outbound connection. This allows us to
	// test that Alpaca doesn't think the routing table has changed just
	// because the port is different on each call to net.Dial(); we only
	// need to consider the outgoing IP address without the port number.
	return mockConn{
		localAddr: &net.UDPAddr{
			IP:   net.IPv4(a, b, c, d),
			Port: rand.IntN(65535-1024) + 1024,
		},
	}
}

func newDialError(network, address, text string) *net.OpError {
	return &net.OpError{
		Op:     "dial",
		Net:    network,
		Source: nil,
		Addr:   mockAddr(address),
		Err:    errors.New(text),
	}
}

func TestNetworkMonitor(t *testing.T) {
	var network mockNet
	nm := &netMonitorImpl{getAddrs: network.interfaceAddrs, dial: network.dial}
	network.state = "offline"
	assert.True(t, nm.addrsChanged())
	network.state = "wifi"
	assert.True(t, nm.addrsChanged())
	assert.False(t, nm.addrsChanged())
	network.state = "vpn"
	assert.True(t, nm.addrsChanged())
	network.state = "offline"
	assert.True(t, nm.addrsChanged())
}
