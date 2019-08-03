package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type proxyFinderTestServer struct {
	addrs []net.Addr
	proxy string
}

func (s proxyFinderTestServer) serverIsReachableFromClient() bool {
	for _, addr := range s.addrs {
		if strings.HasPrefix(addr.String(), "10.") {
			// For the purposes of these tests, pretend that the proxy is reachable only
			// if the client has an IP address in the 10.0.0.0/8 block.
			return true
		}
	}
	return false
}

func (s proxyFinderTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.serverIsReachableFromClient() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `function FindProxyForURL(url, host) { return "PROXY %s" }`, s.proxy)
}

func checkProxyForURL(t *testing.T, pf *ProxyFinder, rawURL string, expectedProxy *url.URL) {
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req = req.WithContext(context.WithValue(req.Context(), "id", 0))
	proxy, err := pf.findProxyForRequest(req)
	require.Nil(t, err)
	assert.Equal(t, expectedProxy, proxy)
}

func TestProxyFinder(t *testing.T) {
	// Initially, we're not on the network, and only have a loopback address.
	s := &proxyFinderTestServer{toAddrs("127.0.0.1"), "proxy.anz.com:8080"}
	pacServer := httptest.NewServer(s)
	defer pacServer.Close()
	pf := newProxyFinder(pacServer.URL, func() ([]net.Addr, error) { return s.addrs, nil }, net.DefaultResolver.LookupAddr)
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", nil)
	// Connect to a corporate WiFi, and get a 10.0.0.0/8 address.
	s.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	proxy := &url.URL{Host: "proxy.anz.com:8080"}
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", proxy)
	// Tether, and get a 192.168.0.0/16 address.
	s.addrs = toAddrs("127.0.0.1", "192.168.1.2")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", nil)
	// Get back on the corporate WiFi.
	s.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", proxy)
}

func TestProxyWithoutPort(t *testing.T) {
	s := &proxyFinderTestServer{toAddrs("10.0.0.1"), "proxy.anz.com"}
	pacServer := httptest.NewServer(s)
	defer pacServer.Close()
	pf := newProxyFinder(pacServer.URL, net.InterfaceAddrs, net.DefaultResolver.LookupAddr)
	checkProxyForURL(t, pf, "https://www.anz.com.au/", &url.URL{Host: "proxy.anz.com:80"})
}

func TestPacFromFilesystem(t *testing.T) {
	// Set up a test PAC file
	proxy := &url.URL{Host: "proxy.example.com:80"}
	content := []byte(fmt.Sprintf(`function FindProxyForURL(url, host) { return "PROXY %s"}`, proxy.Host))
	tmpfile, err := ioutil.TempFile("", "test.pac")
	require.Nil(t, err)
	defer os.Remove(tmpfile.Name())
	ioutil.WriteFile(tmpfile.Name(), content, 0644)
	pacLocation := fmt.Sprintf("file://%s", tmpfile.Name())

	s := &proxyFinderTestServer{toAddrs("127.0.0.1"), "proxy.anz.com"}
	pacServer := httptest.NewServer(s)
	defer pacServer.Close()

	pf := newProxyFinder(pacLocation,
		func() ([]net.Addr, error) { return s.addrs, nil },
		func(ctx context.Context, addr string) ([]string, error) {
			// if the proxy is online assume DNS is offline
			if s.serverIsReachableFromClient() {
				return nil, errors.New("Failed to resolve")
			} else {
				return []string{"hostname"}, nil
			}
		})

	// Initially, we're not on the network, and only have a loopback address.
	checkProxyForURL(t, pf, "https://www.anz.com.au/", nil)
	// Connect to a corporate WiFi, and get a 10.0.0.0/8 address.
	s.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", proxy)
	// Tether, and get a 192.168.0.0/16 address.
	s.addrs = toAddrs("127.0.0.1", "192.168.1.2")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", nil)
	// Get back on the corporate WiFi.
	s.addrs = toAddrs("127.0.0.1", "10.20.30.40")
	checkProxyForURL(t, pf, "https://www.anz.com.au/personal/", proxy)
}
