// Copyright 2019, 2021 The Alpaca Authors
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
	"io/ioutil"
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
	tempdir, err := ioutil.TempDir("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	pacPath := path.Join(tempdir, "test.pac")
	require.NoError(t, ioutil.WriteFile(pacPath, content, 0644))
	pacURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(pacPath)}
	pf := newPACFetcher(pacURL.String())
	assert.Equal(t, content, pf.download())
	assert.True(t, pf.isConnected())
	require.NoError(t, os.Remove(pacPath))
	assert.Nil(t, pf.download())
	assert.False(t, pf.isConnected())
}
