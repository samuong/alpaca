package main

import (
	"log"
	"net/http"
	"net/url"
)

type DirectFallback struct {
	pacUrl string
	pf     proxyFinder
	client *http.Client
	nm     *NetMonitor
	online bool
}

func NewDirectFallback(pacUrl string) (*DirectFallback, error) {
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
	return ps, nil
}

func (ps *DirectFallback) FindProxyForURL(u *url.URL) (string, error) {
	// TODO: this is probably not thread-safe; put a lock around it
	if ps.nm.AddrsChanged() {
		ps.downloadPacFile()
	}
	if !ps.online {
		return "DIRECT", nil
	}
	log.Printf("calling pac runner to find proxy\n")
	return ps.pf.FindProxyForURL(u)
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
