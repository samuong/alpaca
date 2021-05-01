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
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var errNoProxiesAvailable = errors.New("no proxies available")

type proxyFinder struct {
	runner  *pacRunner
	fetcher *pacFetcher
	wrapper *pacWrapper
	blocked *blocklist
	sync.Mutex
}

func newProxyFinder(pacurl string, wrapper *pacWrapper) *proxyFinder {
	pf := &proxyFinder{wrapper: wrapper, blocked: newBlocklist()}
	if len(pacurl) == 0 {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
	} else if _, err := url.Parse(pacurl); err != nil {
		log.Fatalf("Couldn't find a valid PAC URL: %v", pacurl)
	} else {
		pf.runner = new(pacRunner)
		pf.fetcher = newPACFetcher(pacurl)
		pf.checkForUpdates()
	}
	return pf
}

func (pf *proxyFinder) wrapHandler(next http.Handler) http.Handler {
	// If we don't have a fetcher, don't wrap the handler as there's nothing to do.
	if pf.fetcher == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		pf.checkForUpdates()
		next.ServeHTTP(w, req)
	})
}

func (pf *proxyFinder) checkForUpdates() {
	pf.Lock()
	defer pf.Unlock()
	var pacjs []byte
	pacjs = pf.fetcher.download()
	if pacjs == nil {
		if !pf.fetcher.isConnected() {
			pf.blocked = newBlocklist()
			pf.wrapper.wrap(nil)
		}
		return
	}
	pf.blocked = newBlocklist()
	if err := pf.runner.update(pacjs); err != nil {
		log.Printf("Error running PAC JS: %q", err)
	} else {
		pf.wrapper.wrap(pacjs)
	}
}

func (pf *proxyFinder) findProxyForRequest(req *http.Request) (*url.URL, error) {
	id := req.Context().Value(contextKeyID)
	if pf.fetcher == nil {
		log.Printf(`[%d] %s %s via "DIRECT"`, id, req.Method, req.URL)
		return nil, nil
	}
	if !pf.fetcher.isConnected() {
		log.Printf(`[%d] %s %s via "DIRECT" (not connected)`, id, req.Method, req.URL)
		return nil, nil
	}
	str, err := pf.runner.findProxyForURL(req.URL)
	if err != nil {
		return nil, err
	}
	arr := strings.Split(str, ";")
	for _, elem := range arr {
		fields := strings.Fields(strings.TrimSpace(elem))
		if len(fields) == 1 && fields[0] == "DIRECT" {
			log.Printf("[%d] %s %s via %q", id, req.Method, req.URL, elem)
			return nil, nil
		} else if len(fields) == 2 && fields[0] == "PROXY" {
			// The specified proxy should contain both a host and a port, but if for
			// some reason it doesn't, assume port 80. This needs to be made explicit,
			// as it eventually gets passed to net.Dial, which also requires a port.
			proxy := &url.URL{Host: fields[1]}
			if proxy.Port() == "" {
				proxy.Host = net.JoinHostPort(proxy.Host, "80")
			}
			if pf.blocked.contains(proxy.Host) {
				log.Printf("[%d] Skipping bad proxy: %q", id, proxy.Host)
				continue
			}
			log.Printf("[%d] %s %s via %q", id, req.Method, req.URL, elem)
			return proxy, nil
		}
		log.Printf("[%d] Coudln't parse proxy: %q", id, elem)
	}
	return nil, errNoProxiesAvailable
}

func (pf *proxyFinder) blockProxy(proxy string) {
	pf.blocked.add(proxy)
}
