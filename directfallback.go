package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type DirectFallback struct {
	pacUrl string
	pf     *PacRunner
	client *http.Client
	nm     *NetMonitor
	online bool
}

func NewDirectFallback(pacUrl string) *DirectFallback {
	ps := &DirectFallback{
		pacUrl: pacUrl,
		// http.DefaultClient looks at the http(s)_proxy environment variable, which could
		// be pointing at this instance of alpaca. This will either not be running yet,
		// which means we'll fail to proxy the request, or it will be running, which means
		// we'll proxy it to ourselves infinitely. Use a no-proxy client instead.
		client: &http.Client{Transport: &http.Transport{Proxy: nil}},
		nm:     NewNetMonitor(),
		online: false,
	}
	ps.downloadPacFile()
	return ps
}

func (ps *DirectFallback) findProxyForRequest(r *http.Request) (*url.URL, error) {
	// TODO: this is probably not thread-safe; put a lock around it
	if ps.nm.AddrsChanged() {
		ps.downloadPacFile()
	}
	if !ps.online {
		return nil, nil
	}
	s, err := ps.pf.FindProxyForURL(r.URL)
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

func (ps *DirectFallback) downloadPacFile() {
	log.Printf("Downloading proxy auto-config file: %s\n", ps.pacUrl)
	resp, err := ps.client.Get(ps.pacUrl)
	if err != nil {
		log.Printf("error downloading pac file: %s\n", err.Error())
		log.Printf("falling back to direct proxy")
		ps.online = false
		return
	}
	defer resp.Body.Close()
	log.Printf("got a status code of: %s\n", resp.Status)
	if resp.StatusCode != http.StatusOK {
		ps.online = false
		return
	}
	ps.pf, err = NewPacRunner(resp.Body)
	if err != nil {
		log.Printf("error creating new pac runner: %s\n", err.Error())
		log.Printf("falling back to direct proxy")
		ps.online = false
		return
	}
	ps.online = true
	return
}
