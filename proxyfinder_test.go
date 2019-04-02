package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type proxyFinderTestContext struct {
	addrs []net.Addr
}

func (c proxyFinderTestContext) serverIsReachableFromClient() bool {
	for _, addr := range c.addrs {
		if strings.HasPrefix(addr.String(), "10.") {
			return true
		}
	}
	return false
}

func (c proxyFinderTestContext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.serverIsReachableFromClient() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `function FindProxyForURL(url, host) { return "PROXY proxy.anz.com" }`)
}

func checkProxyForURL(t *testing.T, pf *ProxyFinder, rawUrl string, expectedProxy *url.URL) {
	proxy, err := pf.findProxyForRequest(httptest.NewRequest(http.MethodGet, rawUrl, nil))
	require.Nil(t, err)
	assert.Equal(t, expectedProxy, proxy)
}

func TestProxyFinder(t *testing.T) {
	// initially, we're not on the network, and only have a loopback address
	c := &proxyFinderTestContext{toAddrs("127.0.0.1")}
	pacServer := httptest.NewServer(c)
	defer pacServer.Close()
	pf := newProxyFinder(pacServer.URL, func() ([]net.Addr, error) { return c.addrs, nil })
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", nil)
	// connect to a corporate wifi, and get a 10.0.0.0/8 address
	c.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", &url.URL{Host: "proxy.anz.com"})
	// tether, and get a 192.168.0.0/16 address
	c.addrs = toAddrs("127.0.0.1", "192.168.1.2")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", nil)
	// get back on the corporate wifi
	c.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", &url.URL{Host: "proxy.anz.com"})
}
