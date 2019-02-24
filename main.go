package main

import (
	"crypto/tls"
	"log"
	"net/http"
)

func main() {
	proxyHandler, err := NewProxyHandler()
	if err != nil {
		log.Fatal(err)
	}
	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    "localhost:3128",
		Handler: proxyHandler,
		// TODO: Implement HTTP/2 support. In the meantime, set
		// TLSNextProto to a non-nil value to disable HTTP/2.
		TLSNextProto: make(map[string]func(
			*http.Server, *tls.Conn, http.Handler))}
	log.Fatal(s.ListenAndServe())
}
