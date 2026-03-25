package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPACCacheNoFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("function FindProxyForURL() { return 'DIRECT'; }"))
	}))

	pf := newPACFetcher(server.URL)

	data := pf.download()
	if data == nil {
		t.Fatal("expected PAC data on first fetch")
	}

	server.Close()

	data = pf.download()
	if data != nil {
		t.Fatal("expected nil when server is down (no cache fallback)")
	}
}
