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
		"Context":      {http.StatusOK, AddContextID, "[0] 200 GET /"},
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
