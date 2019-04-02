package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The DefaultClient in net/http uses the proxy specified in the http(s)_proxy environment variable,
// which could be pointing at this instance of alpaca. When fetching the PAC file, we always use a
// client that goes directly to the server, rather than via a proxy.
var noProxyClient = &http.Client{Transport: &http.Transport{Proxy: nil}}

type ProxyFinder struct {
	pacURL     string
	pacRunner  *PacRunner
	netMonitor *NetMonitor
	online     bool
}

func NewProxyFinder(pacURL string) *ProxyFinder {
	return newProxyFinder(pacURL, net.InterfaceAddrs)
}

func newProxyFinder(pacURL string, getAddrs addressProvider) *ProxyFinder {
	pf := &ProxyFinder{pacURL: pacURL, netMonitor: NewNetMonitor(getAddrs)}
	pf.downloadPacFile()
	return pf
}

func (pf *ProxyFinder) findProxyForRequest(r *http.Request) (*url.URL, error) {
	// TODO: this is probably not thread-safe; put a lock around it
	if pf.netMonitor.AddrsChanged() {
		pf.downloadPacFile()
	}
	if !pf.online {
		return nil, nil
	}
	s, err := pf.pacRunner.FindProxyForURL(r.URL)
	if err != nil {
		return nil, err
	}
	ss := strings.Split(s, ";")
	if len(ss) > 1 {
		log.Printf("warning: ignoring all but first proxy in '%s'", s)
	}
	trimmed := strings.TrimSpace(ss[0])
	if trimmed == "DIRECT" {
		return nil, nil
	}
	var host string
	n, err := fmt.Sscanf(trimmed, "PROXY %s", &host)
	if err == nil && n == 1 {
		return &url.URL{Host: host}, nil
	}
	n, err = fmt.Sscanf(trimmed, "SOCKS %s", &host)
	if err == nil && n == 1 {
		log.Printf("warning: ignoring socks proxy '%s'", host)
		return nil, nil
	}
	log.Printf("warning: couldn't parse pac response '%s'", s)
	return nil, err
}

func (pf *ProxyFinder) downloadPacFile() {
	log.Printf("Downloading proxy auto-config file: %s\n", pf.pacURL)
	resp, err := noProxyClient.Get(pf.pacURL)
	if err != nil {
		log.Printf("error downloading pac file: %s\n", err.Error())
		log.Printf("falling back to direct proxy")
		pf.online = false
		return
	}
	defer resp.Body.Close()
	log.Printf("got a status code of: %s\n", resp.Status)
	if resp.StatusCode != http.StatusOK {
		pf.online = false
		return
	}
	pf.pacRunner, err = NewPacRunner(resp.Body)
	if err != nil {
		log.Printf("error creating new pac runner: %s\n", err.Error())
		log.Printf("falling back to direct proxy")
		pf.online = false
		return
	}
	pf.online = true
	return
}
