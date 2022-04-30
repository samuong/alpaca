// Copyright 2019, 2021, 2022 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
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

func init() {
	// Set the retry delay to zero, so that it doesn't delay unit tests.
	delayAfterFailedDownload = 0
}

func pacjsHandler(pacjs string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(pacjs)) }
}

type pacServerWhichFailsOnFirstTry struct {
	t     *testing.T
	count int
}

func (s *pacServerWhichFailsOnFirstTry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.count++
	if s.count == 1 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err := w.Write([]byte("test script"))
	require.NoError(s.t, err)
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

func TestDownloadFailsOnFirstTry(t *testing.T) {
	s := &pacServerWhichFailsOnFirstTry{t: t, count: 0}
	server := httptest.NewServer(s)
	defer server.Close()
	pf := newPACFetcher(server.URL)
	require.Equal(t, 0, s.count)
	assert.Equal(t, []byte("test script"), pf.download())
	require.Equal(t, 2, s.count)
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
	tempdir, err := os.MkdirTemp("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	pacPath := path.Join(tempdir, "test.pac")
	require.NoError(t, os.WriteFile(pacPath, content, 0644))
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
