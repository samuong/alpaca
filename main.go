package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	port := flag.Int("p", 3128, "port number to listen on")
	pac := flag.String("C", "", "url of proxy auto-config (pac) file")
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var pf proxyFinder
	if *pac == "" {
		pf = alwaysDirect{}
	} else {
		log.Printf("Downloading proxy auto-config file: %s\n", *pac)
		// http.DefaultClient looks at the http(s)_proxy environment
		// variable, which could be pointing at this not-yet-launched
		// instance of alpaca. Use a no-proxy client instead.
		client := &http.Client{Transport: &http.Transport{Proxy: nil}}
		resp, err := client.Get(*pac)
		check(err)
		defer resp.Body.Close()
		pf, err = NewPacRunner(resp.Body)
		check(err)
	}

	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    fmt.Sprintf("localhost:%d", *port),
		Handler: NewProxyHandler(pf),
		// TODO: Implement HTTP/2 support. In the meantime, set
		// TLSNextProto to a non-nil value to disable HTTP/2.
		TLSNextProto: make(map[string]func(
			*http.Server, *tls.Conn, http.Handler))}
	log.Printf("Listening on port %d\n", *port)
	check(s.ListenAndServe())
}
