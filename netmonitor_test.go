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
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	connectedTo string
}

func (n *mockNet) interfaceAddrs() ([]net.Addr, error) {
	var addrs []net.Addr
	switch n.connectedTo {
	case "vpn", "wifi":
		addrs = append(addrs, toAddrs("192.168.1.2/24", "fe80::fedc:ba98:7654:3210/64")...)
		fallthrough
	case "offline":
		addrs = append(addrs, toAddrs("127.0.0.1/8", "::1/128")...)
	default:
		panic("interfaceAddrs connectedTo=" + n.connectedTo)
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
		return nil, &net.OpError{
			Op:     "dial",
			Net:    network,
			Source: nil,
			Addr:   mockAddr(address),
			Err:    errors.New("connect: no route to host"),
		}
	}
	switch n.connectedTo {
	case "vpn":
		return mockConn{localAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 3)}}, nil
	case "wifi":
		return mockConn{localAddr: &net.UDPAddr{IP: net.IPv4(192, 168, 1, 2)}}, nil
	case "offline":
		return nil, &net.OpError{
			Op:     "dial",
			Net:    network,
			Source: nil,
			Addr:   mockAddr(address),
			Err:    errors.New("connect: network is unreachable"),
		}
	default:
		panic("dial connectedTo=" + n.connectedTo)
	}
}

func TestNetworkMonitor(t *testing.T) {
	var network mockNet
	nm := &netMonitorImpl{getAddrs: network.interfaceAddrs, dial: network.dial}
	network.connectedTo = "offline"
	assert.True(t, nm.addrsChanged())
	network.connectedTo = "wifi"
	assert.True(t, nm.addrsChanged())
	assert.False(t, nm.addrsChanged())
	network.connectedTo = "vpn"
	fmt.Println(`network.connectedTo = "vpn"`)
	assert.True(t, nm.addrsChanged())
	fmt.Println(`network.connectedTo = "offline"`)
	network.connectedTo = "offline"
	assert.True(t, nm.addrsChanged())
}

func TestDumpAddrs(t *testing.T) {
	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	t.Log("---------- INTERFACES AND ADDRESSES: ----------")
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		require.NoError(t, err)
		t.Logf("%s -> %q", iface.Name, addrs)
	}
	remotes := []string{
		"8.8.8.8", "2001:4860:4860::8888", // public addresses
		"10.0.0.0", "172.16.0.0", "192.168.0.0", "FC00::", // private addresses
	}
	t.Log("---------- ROUTES: ----------")
	for _, addr := range remotes {
		conn, err := net.Dial("udp", net.JoinHostPort(addr, "80"))
		if err != nil {
			t.Logf("%q => error: %v\n", addr, err)
			continue
		}
		t.Logf("%q => %q\n", addr, conn.LocalAddr().String())
	}
}
