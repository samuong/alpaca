// Copyright 2019 The Alpaca Authors
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
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestLogger(t *testing.T) {
	tests := map[string]struct {
		status  int
		wrapper func(http.Handler) http.Handler
		out     string
	}{
		"No Status":    {0, nil, "[<nil>] 200 GET /"},
		"Given Status": {http.StatusNotFound, nil, "[<nil>] 404 GET /"},
		"Context":      {http.StatusOK, AddContextID, "[1] 200 GET /"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := &bytes.Buffer{}
			log.SetOutput(b)
			hfunc := func(w http.ResponseWriter, req *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
			}
			var handler http.Handler = http.HandlerFunc(hfunc)
			handler = RequestLogger(handler)
			if tt.wrapper != nil {
				handler = tt.wrapper(handler)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := http.Get(server.URL)
			require.NoError(t, err)
			log.SetOutput(os.Stderr)
			assert.Contains(t, b.String(), tt.out)
		})
	}
}
