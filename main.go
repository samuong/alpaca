package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	port := flag.Int("p", 3128, "port number to listen on")
	pacURLFromFlag := flag.String("C", "", "url of proxy auto-config (pac) file")
	flag.Parse()

	pacURL := *pacURLFromFlag
	if len(pacURL) == 0 {
		var err error
		pacURL, err = findPACURL()
		if err != nil {
			log.Fatalf("Error while trying to detect PAC URL: %v", err)
		}
	}

	var handler ProxyHandler
	if len(pacURL) == 0 {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
		handler = NewProxyHandler(func(req *http.Request) (*url.URL, error) {
			log.Printf(`[%d] %s %s via "DIRECT"`,
				req.Context().Value("id"), req.Method, req.URL)
			return nil, nil
		})
	} else if _, err := url.Parse(pacURL); err != nil {
		log.Fatalf("Couldn't find a valid PAC URL: %v", pacURL)
	} else {
		pf := NewProxyFinder(pacURL)
		handler = NewProxyHandler(func(req *http.Request) (*url.URL, error) {
			return pf.findProxyForRequest(req)
		})
	}

	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    fmt.Sprintf("localhost:%d", *port),
		Handler: handler,
		// TODO: Implement HTTP/2 support. In the meantime, set TLSNextProto to a non-nil
		// value to disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Printf("Listening on port %d\n", *port)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
