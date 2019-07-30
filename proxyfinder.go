package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The maximum size (in bytes) allowed for a PAC script. This matches the limit in Chrome.
const maxResponseBytes = 1 * 1024 * 1024

type ProxyFinder struct {
	pacURL     string
	pacRunner  PACRunner
	netMonitor *NetMonitor
	online     bool
	lock       sync.Mutex
	client     *http.Client
}

func NewProxyFinder(pacURL string) *ProxyFinder {
	return newProxyFinder(pacURL, net.InterfaceAddrs)
}

func newProxyFinder(pacURL string, getAddrs addressProvider) *ProxyFinder {
	// The DefaultClient in net/http uses the proxy specified in the http(s)_proxy environment variable,
	// which could be pointing at this instance of alpaca. When fetching the PAC file, we always use a
	// client that goes directly to the server, rather than via a proxy.
	client := &http.Client{Timeout: 30 * time.Second}
	if strings.HasPrefix(pacURL, "file:") {
		log.Printf("Warning: The PAC URL is served over file://, which is supported by Alpaca, but not by Windows and macOS. Be careful if you configure your system settings to use the same file:// URL, it may not work.")
		client.Transport = http.NewFileTransport(http.Dir("/"))
	} else {
		// The DefaultClient in net/http uses....
		client.Transport = &http.Transport{Proxy: nil}
	}
	pf := &ProxyFinder{pacURL: pacURL, netMonitor: NewNetMonitor(getAddrs), client: client}
	pf.downloadPACFile()
	return pf
}

func (pf *ProxyFinder) findProxyForRequest(req *http.Request) (*url.URL, error) {
	pf.lock.Lock()
	if pf.netMonitor.AddrsChanged() {
		pf.downloadPACFile()
	}
	pf.lock.Unlock()
	id := req.Context().Value("id")
	if !pf.online {
		log.Printf(`[%d] %s %s via "DIRECT"`, id, req.Method, req.URL)
		return nil, nil
	}
	s, err := pf.pacRunner.FindProxyForURL(req.URL)
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
	n, err = fmt.Sscanf(trimmed, "SOCKS %s", &host)
	if err == nil && n == 1 {
		log.Printf("[%d] Warning: ignoring SOCKS proxy %q", id, host)
		return nil, nil
	}
	log.Printf("[%d] Couldn't parse PAC response %q", id, s)
	return nil, err
}

func (pf *ProxyFinder) downloadPACFile() {

	resp, err := pf.client.Get(pf.pacURL)
	if err != nil {
		log.Printf("Error downloading PAC file: %q\n", err)
		pf.online = false
		return
	}
	defer resp.Body.Close()
	log.Printf("GET %q returned %q\n", pf.pacURL, resp.Status)
	if resp.StatusCode != http.StatusOK {
		pf.online = false
		return
	}
	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, resp.Body, maxResponseBytes); err != nil && err != io.EOF {
		log.Printf("Error reading PAC JS from response body: %q\n", err)
		pf.online = false
		return
	}
	if err := pf.pacRunner.Update(buf.Bytes()); err != nil {
		log.Printf("Error running PAC JS: %q\n", err)
		pf.online = false
		return
	}

	if strings.HasPrefix(pf.pacURL, "file:") {
		// When using a local PAC file the online/offline status can't be detirmined by the fact the PAC file is returned
		// Instead try reverse DNS resolution of an internet address
		const timeout = 2 * time.Second
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel() // important to avoid a resource leak
		var r net.Resolver
		_, err1 := r.LookupAddr(ctx, "8.8.8.8")
		_, err2 := r.LookupAddr(ctx, "2001:4860:4860::8888")
		if err1 == nil || err2 == nil {
			log.Printf("Successfully resolved internet DNS address.  Bypassing proxy")
			pf.online = false
			return
		}
	}

	pf.online = true
	return
}
