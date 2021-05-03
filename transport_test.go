// Copyright 2021 The Alpaca Authors
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
	"bufio"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransport(t *testing.T) {
	handler := func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("It works!"))
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	var tr transport
	require.NoError(t, tr.dial("tcp", server.Listener.Addr().String()))
	defer tr.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	t.Run("RoundTrip", func(t *testing.T) {
		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "It works!", string(body))
	})

	t.Run("Hijack", func(t *testing.T) {
		conn := tr.hijack()
		defer conn.Close()
		require.NoError(t, req.Write(conn))
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "It works!", string(body))
		assert.NoError(t, tr.Close())
	})
}

func TestTransportErrors(t *testing.T) {
	var tr transport
	req, err := http.NewRequest(http.MethodGet, "http://alpaca.test", nil)
	require.NoError(t, err)

	t.Run("NotConnected", func (t *testing.T) {
		_, err = tr.RoundTrip(req)
		assert.Error(t, err)
	})

	t.Run("Closed", func (t *testing.T) {
		require.NoError(t, tr.Close())
		_, err = tr.RoundTrip(req)
		assert.Error(t, err)
	})

	t.Run("CloseTwice", func (t *testing.T) {
		assert.NoError(t, tr.Close())
		assert.NoError(t, tr.Close())
	})
}
