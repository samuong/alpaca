package main

import (
	"crypto/tls"
	"log"
	"net/http"
)

func main() {
	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    "localhost:3128",
		Handler: http.HandlerFunc(proxyHandler),
		// TODO: Implement HTTP/2 support. In the meantime, set
		// TLSNextProto to a non-nil value to disable HTTP/2.
		TLSNextProto: make(map[string]func(
			*http.Server, *tls.Conn, http.Handler))}
	log.Fatal(s.ListenAndServe())
}
