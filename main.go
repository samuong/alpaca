package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "warning: alpaca is still under heavy development and command-line arguments will probably change in the future, so please don't get too attached to them :)")
	}

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
			log.Printf("%v Disabling proxy authentication.\n", err)
		} else {
			log.Printf("Found NoMAD credentails for %s\\%s in system keychain\n",
				tmp.domain, tmp.username)
			a = tmp
		}
	}

	var proxyHandler ProxyHandler
	if len(pacURL) == 0 {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
		proxyHandler = NewProxyHandler(func(req *http.Request) (*url.URL, error) {
			log.Printf(`[%d] %s %s via "DIRECT"`,
				req.Context().Value("id"), req.Method, req.URL)
			return nil, nil
		}, nil)
	} else if _, err := url.Parse(pacURL); err != nil {
		log.Fatalf("Couldn't find a valid PAC URL: %v", pacURL)
	} else {
		pf := NewProxyFinder(pacURL)
		proxyHandler = NewProxyHandler(func(req *http.Request) (*url.URL, error) {
			return pf.findProxyForRequest(req)
		}, &a)
	}

	mux := http.NewServeMux()
	s := &http.Server{
		// Set the addr to localhost so that we only listen locally.
		Addr:    fmt.Sprintf("localhost:%d", *port),
		Handler: AddContextID(proxyHandler.WrapHandler(mux)),
		// TODO: Implement HTTP/2 support. In the meantime, set TLSNextProto to a non-nil
		// value to disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Printf("Listening on port %d\n", *port)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
