// Copyright 2019, 2021 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
)

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
	pacURLFromFlag := flag.String("C", "", "URL of proxy auto-config (PAC) file")

	domain := flag.String("d", "", "AD domain/Kerberos realm of the proxy account")
	username := flag.String("u", whoAmI(), "username of the proxy account")

	ntlm := flag.Bool("ntlm", false, "enable NTLM authentication")
	printHash := flag.Bool("H", false, "print hashed NTLM credentials for non-interactive use")

	krb5 := flag.Bool("krb5", false, "enable Kerberos authentication")
	krb5conf := flag.String("krb5conf", "", "path to krb5.conf file")
	kdc := flag.String("kdc", "", "address of key distribution center (KDC)")

	flag.Parse()

	pacURL := *pacURLFromFlag
	if len(pacURL) == 0 {
		var err error
		pacURL, err = findPACURL()
		if err != nil {
			log.Fatalf("Error while trying to detect PAC URL: %v", err)
		}
	}

	var src credentialSource
	if *domain != "" {
		src = fromTerminal().forUser(*domain, *username)
	} else if value := os.Getenv("NTLM_CREDENTIALS"); value != "" && !*krb5 {
		src = fromEnvVar(value)
	} else if keyringSupported {
		src = fromKeyring()
	}

	var creds credentials
	if (*ntlm || *krb5) && src != nil {
		var err error
		creds, err = src.getCredentials(*ntlm, *krb5, *krb5conf, *kdc)
		if err != nil {
			fmt.Printf("Credentials not found, disabling proxy auth: %v", err)
		}
	}

	if *printHash {
		if creds.ntlm == nil {
			fmt.Println("Please specify a domain (using -d) and username (using -u)")
			os.Exit(1)
		}
		fmt.Printf("# Add this to your ~/.profile (or equivalent) and restart your shell\n")
		fmt.Printf("NTLM_CREDENTIALS=%q; export NTLM_CREDENTIALS\n", creds.ntlm)
		os.Exit(0)
	}

	pacWrapper := NewPACWrapper(PACData{Port: *port})
	proxyFinder := NewProxyFinder(pacURL, pacWrapper)
	proxyHandler := NewProxyHandler(
		proxyFinder.findProxyForRequest, creds, proxyFinder.blockProxy)
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
