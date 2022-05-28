// Copyright 2019, 2021, 2022 The Alpaca Authors
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
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const contextKeyProxy = contextKey("proxy")

func getProxyFromContext(req *http.Request) (*url.URL, error) {
	if value := req.Context().Value(contextKeyProxy); value != nil {
		proxy := value.(*url.URL)
		return proxy, nil
	}
	return nil, nil
}

type ProxyFinder struct {
	runner  *PACRunner
	fetcher *pacFetcher
	wrapper *PACWrapper
	blocked *blocklist
	sync.Mutex
}

func NewProxyFinder(pacurl string, wrapper *PACWrapper) *ProxyFinder {
	pf := &ProxyFinder{wrapper: wrapper, blocked: newBlocklist()}
	pf.runner = new(PACRunner)
	pf.fetcher = newPACFetcher(pacurl)
	pf.checkForUpdates()
	return pf
}

func (pf *ProxyFinder) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		pf.checkForUpdates()
		proxy, err := pf.findProxyForRequest(req)
		if err != nil {
			log.Printf("[%d] %v", req.Context().Value(contextKeyID), err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if proxy != nil {
			ctx := context.WithValue(req.Context(), contextKeyProxy, proxy)
			req = req.WithContext(ctx)
		}
		next.ServeHTTP(w, req)
	})
}

func (pf *ProxyFinder) checkForUpdates() {
	pf.Lock()
	defer pf.Unlock()
	pacjs := pf.fetcher.download()
	if pacjs == nil {
		if !pf.fetcher.isConnected() {
			pf.blocked = newBlocklist()
			pf.wrapper.Wrap(nil)
		}
		return
	}
	pf.blocked = newBlocklist()
	if err := pf.runner.Update(pacjs); err != nil {
		log.Printf("Error running PAC JS: %q", err)
	} else {
		pf.wrapper.Wrap(pacjs)
	}
}

func (pf *ProxyFinder) findProxyForRequest(req *http.Request) (*url.URL, error) {
	id := req.Context().Value(contextKeyID)
	if pf.fetcher == nil {
		log.Printf(`[%d] %s %s via "DIRECT"`, id, req.Method, req.URL)
		return nil, nil
	}
	if !pf.fetcher.isConnected() {
		log.Printf(`[%d] %s %s via "DIRECT" (not connected to PAC server)`,
			id, req.Method, req.URL)
		return nil, nil
	}
	str, err := pf.runner.FindProxyForURL(*req.URL)
	if err != nil {
		return nil, err
	}
	var fallback *url.URL
	for _, elem := range strings.Split(str, ";") {
		fields := strings.Fields(strings.TrimSpace(elem))
		var scheme string
		var defaultPort string
		if len(fields) == 0 {
			continue
		} else if fields[0] == "DIRECT" {
			log.Printf("[%d] %s %s via %q", id, req.Method, req.URL, elem)
			return nil, nil
		} else if fields[0] == "PROXY" || fields[0] == "HTTP" {
			scheme = "http"
			defaultPort = "80"
		} else if fields[0] == "HTTPS" {
			scheme = "https"
			defaultPort = "443"
		} else {
			log.Printf("[%d] Couldn't parse proxy: %q", id, elem)
			continue
		}
		proxy := &url.URL{Scheme: scheme, Host: fields[1]}
		if proxy.Port() == "" {
			proxy.Host = net.JoinHostPort(proxy.Host, defaultPort)
		}
		if pf.blocked.contains(proxy.Host) {
			if fallback == nil {
				fallback = proxy
			}
			continue
		}
		log.Printf("[%d] %s %s via %q", id, req.Method, req.URL, elem)
		return proxy, nil
	}
	if fallback != nil {
		// All the proxies are currently blocked. In this case, we'll temporarily ignore the
		// blocklist and fall back to the first proxy that we saw (and skipped).
		return fallback, nil
	}
	return nil, errors.New("no proxies available")
}

func (pf *ProxyFinder) blockProxy(proxy string) {
	pf.blocked.add(proxy)
}
