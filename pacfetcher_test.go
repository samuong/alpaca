// Copyright 2019, 2021, 2022, 2025 The Alpaca Authors
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
	pf.pacFinder = newPacFinder(s2.URL)
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

func TestPacFromFilesystem(t *testing.T) {
	// Set up a test PAC file
	content := []byte(`function FindProxyForURL(url, host) { return "DIRECT" }`)
	tempdir, err := os.MkdirTemp("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	pacPath := path.Join(tempdir, "test.pac")
	require.NoError(t, os.WriteFile(pacPath, content, 0644))
	pacURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(pacPath)}
	pf := newPACFetcher(pacURL.String())
	pf.monitor = newNetMonitor()
	assert.Equal(t, content, pf.download())
	assert.True(t, pf.isConnected())
}

func TestDecodeDataURL(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{
			"Base64",
			"data:application/x-ns-proxy-autoconfig;base64,ZnVuY3Rpb24gRmluZFByb3h5Rm9yVVJMKHVybCwgaG" +
				"9zdCkgewogIHJldHVybiAiUFJPWFkgcHJveHk6ODA4MCI7Cn0K",
			"function FindProxyForURL(url, host) {\n  return \"PROXY proxy:8080\";\n}\n",
		},
		{
			"URLEncoded",
			"data:,function%20FindProxyForURL(url%2C%20host)%20%7B%0A%20%20return%20%22PROXY%20proxy%3A" +
				"8080%22%3B%0A%7D%0A",
			"function FindProxyForURL(url, host) {\n  return \"PROXY proxy:8080\";\n}\n",
		},
		{
			"URLEncodedWithPlus",
			"data:,foo+bar",
			"foo+bar",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pf := newPACFetcher(test.uri)
			assert.Equal(t, test.expected, string(pf.download()))
		})
	}
}

func TestDecodeDataURL_NonDataScheme(t *testing.T) {
	uri := "http://example.com"
	got, err := decodeDataURL(uri)
	assert.Nil(t, got)
	assert.NoError(t, err)
}
