// Copyright 2019, 2021 The Alpaca Authors
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
		log.Printf("Error while getting network interface addresses: %q", err)
		return false
	}
	set := addrSliceToSet(addrs)
	if setsAreEqual(set, nm.addrs) {
		return false
	} else {
		log.Printf("Network changes detected: %v", addrs)
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
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
