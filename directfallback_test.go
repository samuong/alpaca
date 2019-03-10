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

type directFallbackTestContext struct {
	clientAddrs []net.Addr
}

func (c directFallbackTestContext) serverIsReachableFromClient() bool {
	for _, addr := range c.clientAddrs {
		if strings.HasPrefix(addr.String(), "10.") {
			return true
		}
	}
	return false
}

func (c directFallbackTestContext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.serverIsReachableFromClient() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `function FindProxyForURL(url, host) { return "PROXY proxy.anz.com" }`)
}

func checkProxyForURL(t *testing.T, pf proxyFinder, rawUrl, expectedProxy string) {
	u, err := url.Parse(rawUrl)
	require.Nil(t, err)
	proxy, err := pf.FindProxyForURL(u)
	require.Nil(t, err)
	assert.Equal(t, expectedProxy, proxy)
}

func TestDirectFallback(t *testing.T) {
	// initially, we're not on the network, and only have a loopback address
	c := &directFallbackTestContext{toAddrs("127.0.0.1")}
	pacServer := httptest.NewServer(c)
	df, err := NewDirectFallback(pacServer.URL)
	require.Nil(t, err)
	df.nm.p = func() ([]net.Addr, error) { return c.clientAddrs, nil }
	checkProxyForURL(t, df, "https://www.anz.com.au/personal/", "DIRECT")
	// connect to a corporate wifi, and get a 10.0.0.0/8 address
	c.clientAddrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, df, "https://www.anz.com.au/personal/", "PROXY proxy.anz.com")
	// tether, and get a 192.168.0.0/16 address
	c.clientAddrs = toAddrs("127.0.0.1", "192.168.1.2")
	checkProxyForURL(t, df, "https://www.anz.com.au/personal/", "DIRECT")
	// get back on the corporate wifi
	c.clientAddrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, df, "https://www.anz.com.au/personal/", "PROXY proxy.anz.com")
}
