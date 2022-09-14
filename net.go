package main

import (
	"log"
	"net"
)

func networks(hostname string) []string {
	if hostname == "" {
		return []string{"net"}
	}
	addrs, err := net.LookupIP(hostname)
	if err != nil {
		log.Fatal(err)
	}
	nets := make([]string, 0, 2)
	ipv4 := false
	ipv6 := false
	for _, addr := range addrs {
		// addr == net.IPv4len doesn't work because all addrs use IPv6 format.
		if addr.To4() != nil {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	if ipv4 {
		nets = append(nets, "tcp4")
	}
	if ipv6 {
		nets = append(nets, "tcp6")
	}
	return nets
}
