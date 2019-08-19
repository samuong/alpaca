package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type ProxyFinder struct {
	runner  *PACRunner
	fetcher *pacFetcher
	sync.Mutex
}

func NewProxyFinder(pacurl string) *ProxyFinder {
	pf := &ProxyFinder{}
	if len(pacurl) == 0 {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
	} else if _, err := url.Parse(pacurl); err != nil {
		log.Fatalf("Couldn't find a valid PAC URL: %v", pacurl)
	} else {
		pf.runner = new(PACRunner)
		pf.fetcher = newPACFetcher(pacurl)
	}
	pf.checkForUpdates()
	return pf
}

func (pf *ProxyFinder) checkForUpdates() {
	pf.Lock()
	defer pf.Unlock()
	var pacjs []byte
	pacjs = pf.fetcher.download()
	if pacjs == nil {
		return
	}
	if err := pf.runner.Update(pacjs); err != nil {
		log.Printf("Error running PAC JS: %q\n", err)
	}
}

func (pf *ProxyFinder) findProxyForRequest(req *http.Request) (*url.URL, error) {
	id := req.Context().Value("id")
	if pf.fetcher == nil {
		log.Printf(`[%d] %s %s via "DIRECT"`, id, req.Method, req.URL)
		return nil, nil
	}
	pf.checkForUpdates()
	if !pf.fetcher.isConnected() {
		log.Printf(`[%d] %s %s via "DIRECT" (not connected)`, id, req.Method, req.URL)
		return nil, nil
	}
	s, err := pf.runner.FindProxyForURL(req.URL)
	if err != nil {
		return nil, err
	}
	log.Printf("[%d] %s %s via %q", id, req.Method, req.URL, s)
	ss := strings.Split(s, ";")
	if len(ss) > 1 {
		log.Printf("[%d] Warning: ignoring all but first proxy in %q", id, s)
	}
	trimmed := strings.TrimSpace(ss[0])
	if trimmed == "DIRECT" {
		return nil, nil
	}
	var host string
	n, err := fmt.Sscanf(trimmed, "PROXY %s", &host)
	if err == nil && n == 1 {
		// The specified proxy should contain both a host and a port, but if for some reason
		// it doesn't, assume port 80. This needs to be made explicit, as it eventually gets
		// passed to net.Dial, which also requires a port.
		proxy := &url.URL{Host: host}
		if proxy.Port() == "" {
			proxy.Host = net.JoinHostPort(host, "80")
		}
		return proxy, nil
	}
	if strings.HasPrefix(trimmed, "SOCKS ") {
		return nil, errors.New("Alpaca does not yet support SOCKS proxies")
	} else {
		return nil, errors.New("Couldn't parse PAC response")
	}
}
