package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// The maximum size (in bytes) allowed for a PAC script. At 1 MB, this matches the limit in Chrome.
const maxResponseBytes = 1 * 1024 * 1024

type pacFetcher struct {
	pacurl     string
	monitor    netMonitor
	client     *http.Client
	lookupAddr func(context.Context, string) ([]string, error)
	connected  bool
	//cache  []byte
	//modified time.Time
	//fetched time.Time
	//expiry   time.Time
	//etag     string
}

func newPACFetcher(pacurl string) *pacFetcher {
	client := &http.Client{Timeout: 30 * time.Second}
	if strings.HasPrefix(pacurl, "file:") {
		log.Printf("Warning: Alpaca supports file:// PAC URLs, but Windows and macOS don't")
		client.Transport = http.NewFileTransport(http.Dir("/"))
	} else {
		// The DefaultClient in net/http uses the proxy specified in the http(s)_proxy
		// environment variable, which could be pointing at this instance of alpaca. When
		// fetching the PAC file, we always use a client that goes directly to the server,
		// rather than via a proxy.
		client.Transport = &http.Transport{Proxy: nil}
	}
	return &pacFetcher{
		pacurl:     pacurl,
		monitor:    newNetMonitor(),
		client:     client,
		lookupAddr: net.DefaultResolver.LookupAddr,
	}
}

func (pf *pacFetcher) download() []byte {
	if !pf.monitor.addrsChanged() {
		return nil
	}
	pf.connected = false
	resp, err := pf.client.Get(pf.pacurl)
	if err != nil {
		log.Printf("Error downloading PAC file: %q", err)
		return nil
	}
	defer resp.Body.Close()
	log.Printf("GET %q returned %q", pf.pacurl, resp.Status)
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var buf bytes.Buffer
	_, err = io.CopyN(&buf, resp.Body, maxResponseBytes)
	if err == io.EOF {
		if strings.HasPrefix(pf.pacurl, "file:") {
			// When using a local PAC file the online/offline status can't be determined
			// by the fact that the PAC file is returned. Instead try reverse DNS
			// resolution of Google's Public DNS Servers.
			const timeout = 2 * time.Second
			ctx, cancel := context.WithTimeout(context.TODO(), timeout)
			defer cancel()
			_, err1 := pf.lookupAddr(ctx, "8.8.8.8")
			_, err2 := pf.lookupAddr(ctx, "2001:4860:4860::8888")
			if err1 == nil || err2 == nil {
				log.Printf("Successfully resolved public address; bypassing proxy")
			} else {
				pf.connected = true
			}
		} else {
			pf.connected = true
		}
		return buf.Bytes()
	} else if err != nil {
		log.Printf("Error reading PAC JS from response body: %q", err)
		return nil
	} else {
		log.Printf("PAC JS is too big (limit is %d bytes)", maxResponseBytes)
		return nil
	}
}

func (pf *pacFetcher) isConnected() bool {
	return pf.connected
}
