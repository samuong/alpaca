package main

import (
	"log"
	"net"
)

type netMonitor interface {
	addrsChanged() bool
}

type netMonitorImpl struct {
	addrs    map[string]struct{}
	getAddrs func() ([]net.Addr, error)
}

func newNetMonitor() *netMonitorImpl {
	return &netMonitorImpl{getAddrs: net.InterfaceAddrs}
}

func (nm *netMonitorImpl) addrsChanged() bool {
	addrs, err := nm.getAddrs()
	if err != nil {
		log.Printf("Error while getting network interface addresses: %q\n", err)
		return false
	}
	set := addrSliceToSet(addrs)
	if setsAreEqual(set, nm.addrs) {
		return false
	} else {
		log.Printf("Network changes detected: %v\n", addrs)
		nm.addrs = set
		return true
	}
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
	for k, _ := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
