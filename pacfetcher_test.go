package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pacjsHandler(pacjs string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(pacjs)) }
}

type fakeNetMonitor struct {
	changed bool
}

func (nm *fakeNetMonitor) addrsChanged() bool {
	tmp := nm.changed
	nm.changed = false
	return tmp
}

func TestDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(pacjsHandler("test script")))
	defer server.Close()
	pf := newPACFetcher(server.URL)
	assert.Equal(t, []byte("test script"), pf.download())
	assert.True(t, pf.isConnected())
}

func TestDownloadWithNetworkChanges(t *testing.T) {
	// Initially, the download succeeds and we are connected (to the PAC server).
	s1 := httptest.NewServer(http.HandlerFunc(pacjsHandler("test script 1")))
	nm := &fakeNetMonitor{true}
	pf := newPACFetcher(s1.URL)
	pf.monitor = nm
	assert.Equal(t, []byte("test script 1"), pf.download())
	assert.True(t, pf.isConnected())
	// Try again. Nothing changed, so we don't get a new script, but are still connected.
	assert.Nil(t, pf.download())
	assert.True(t, pf.isConnected())
	// Disconnect from the network.
	s1.Close()
	nm.changed = true
	assert.Nil(t, pf.download())
	assert.False(t, pf.isConnected())
	// Connect to a new network.
	s2 := httptest.NewServer(http.HandlerFunc(pacjsHandler("test script 2")))
	defer s2.Close()
	nm.changed = true
	pf.pacurl = s2.URL
	assert.Equal(t, []byte("test script 2"), pf.download())
	assert.True(t, pf.isConnected())
}

func TestResponseLimit(t *testing.T) {
	bigscript := strings.Repeat("x", 2*1024*1024) // 2 MB
	server := httptest.NewServer(http.HandlerFunc(pacjsHandler(bigscript)))
	defer server.Close()
	pf := newPACFetcher(server.URL)
	assert.Nil(t, pf.download())
	assert.False(t, pf.isConnected())
}

type testNetwork struct {
	connected bool
}

func (tn testNetwork) InterfaceAddrs() ([]net.Addr, error) {
	addr := func(s string) *net.IPAddr { return &net.IPAddr{IP: net.ParseIP(s)} }
	if tn.connected {
		return []net.Addr{addr("127.0.0.1"), addr("192.0.2.1")}, nil
	} else {
		return []net.Addr{addr("127.0.0.1"), addr("198.51.100.1")}, nil
	}
}

func (tn testNetwork) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	if tn.connected {
		return []string{}, fmt.Errorf("lookup %s: Name or service not known", addr)
	} else {
		return []string{"dns.google."}, nil
	}
}

func TestPacFromFilesystem(t *testing.T) {
	// Set up a test PAC file
	content := []byte(`function FindProxyForURL(url, host) { return "DIRECT" }`)
	tempdir, err := ioutil.TempDir("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	pacPath := path.Join(tempdir, "test.pac")
	require.NoError(t, ioutil.WriteFile(pacPath, content, 0644))
	pacURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(pacPath)}

	tn := testNetwork{false}
	pf := newPACFetcher(pacURL.String())
	pf.monitor = &netMonitorImpl{
		getAddrs: func() ([]net.Addr, error) { return tn.InterfaceAddrs() },
	}
	pf.lookupAddr = func(ctx context.Context, addr string) ([]string, error) {
		return tn.LookupAddr(ctx, addr)
	}

	assert.Equal(t, content, pf.download())
	assert.False(t, pf.isConnected())
	tn.connected = true
	assert.Equal(t, content, pf.download())
	assert.True(t, pf.isConnected())
}
