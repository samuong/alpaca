package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"

	"golang.org/x/crypto/ssh/terminal"
)

var getCredentialsFromKeyring func() (authenticator, error)

func whoAmI() string {
	me, err := user.Current()
	if err != nil {
		return ""
	}
	return me.Username
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	port := flag.Int("p", 3128, "port number to listen on")
	pacURLFromFlag := flag.String("C", "", "url of proxy auto-config (pac) file")
	domain := flag.String("d", "", "domain of the proxy account (for NTLM auth)")
	username := flag.String("u", whoAmI(), "username of the proxy account (for NTLM auth)")
	flag.Parse()

	pacURL := *pacURLFromFlag
	if len(pacURL) == 0 {
		var err error
		pacURL, err = findPACURL()
		if err != nil {
			log.Fatalf("Error while trying to detect PAC URL: %v", err)
		}
	}

	var a authenticator
	if *domain != "" {
		fmt.Printf("Password (for %s\\%s): ", *domain, *username)
		buf, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("Error reading password from stdin: %v", err)
		}
		a = authenticator{domain: *domain, username: *username, password: string(buf)}
	} else if getCredentialsFromKeyring != nil {
		tmp, err := getCredentialsFromKeyring()
		if err != nil {
			log.Printf("NoMAD credentials not found, disabling proxy auth: %v", err)
		} else {
			log.Printf("Found NoMAD credentails for %s\\%s in system keychain",
				tmp.domain, tmp.username)
			a = tmp
		}
	}

	pacWrapper := NewPACWrapper(PACData{Port: *port})
	proxyFinder := NewProxyFinder(pacURL, pacWrapper)
	proxyHandler := NewProxyHandler(proxyFinder.findProxyForRequest, &a)
	mux := http.NewServeMux()
	pacWrapper.SetupHandlers(mux)

	// build the handler by wrapping middleware upon middleware
	var handler http.Handler = mux
	handler = RequestLogger(handler)
	handler = proxyHandler.WrapHandler(handler)
	handler = proxyFinder.WrapHandler(handler)
	handler = AddContextID(handler)

	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    fmt.Sprintf("localhost:%d", *port),
		Handler: handler,
		// TODO: Implement HTTP/2 support. In the meantime, set TLSNextProto to a non-nil
		// value to disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Printf("Listening on port %d", *port)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
