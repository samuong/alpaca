// Copyright 2019, 2022 The Alpaca Authors
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
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getIDFromRequest(t *testing.T, server *httptest.Server) uint {
	res, err := http.Get(server.URL)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	id, err := strconv.ParseUint(string(b), 10, 64)
	require.NoError(t, err)
	return uint(id)
}

func TestContextID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := r.Context().Value(contextKeyID).(uint64)
		assert.True(t, ok, "Unexpected type for context id value")
		_, err := w.Write([]byte(strconv.FormatUint(uint64(id), 10)))
		require.NoError(t, err)
	})
	server := httptest.NewServer(AddContextID(handler))
	defer server.Close()
	assert.Equal(t, uint(1), getIDFromRequest(t, server))
	assert.Equal(t, uint(2), getIDFromRequest(t, server))
}
