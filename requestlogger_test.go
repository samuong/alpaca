package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
			var handler http.Handler // nolint:gosimple
			handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
			})
			handler = RequestLogger(handler)
			if tt.wrapper != nil {
				handler = tt.wrapper(handler)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := http.Get(server.URL)
			assert.NoError(t, err)
			log.SetOutput(os.Stderr)
			assert.Contains(t, b.String(), tt.out)
		})
	}
}
