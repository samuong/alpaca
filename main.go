package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	portPtr := flag.Int("p", 3128, "port number to listen on")
	pacUrlPtr := flag.String("C", "", "url of proxy auto-config (pac) file")
	flag.Parse()
	var proxyHandler http.Handler
	var err error
	if *pacUrlPtr == "" {
		proxyHandler, err = NewDirectProxyHandler()
	} else {
		log.Printf("Downloading proxy auto-config: %s\n", *pacUrlPtr)
		proxyHandler, err = NewProxyHandler(*pacUrlPtr)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Listening on port %d\n", *portPtr)
	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    fmt.Sprintf("localhost:%d", *portPtr),
		Handler: proxyHandler,
		// TODO: Implement HTTP/2 support. In the meantime, set
		// TLSNextProto to a non-nil value to disable HTTP/2.
		TLSNextProto: make(map[string]func(
			*http.Server, *tls.Conn, http.Handler))}
	log.Fatal(s.ListenAndServe())
}
