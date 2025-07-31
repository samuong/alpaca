// Copyright 2019, 2021, 2022, 2025 The Alpaca Authors
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
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
)

var BuildVersion string

func whoAmI() string {
	me, err := user.Current()
	if err != nil {
		return ""
	}
	return me.Username
}

type stringArrayFlag []string

func (s *stringArrayFlag) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringArrayFlag) Set(value string) error {
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	var hosts stringArrayFlag
	flag.Var(&hosts, "l", "address to listen on")
	port := flag.Int("p", 3128, "port number to listen on")
	pacurl := flag.String("C", "", "url of proxy auto-config (pac) file")
	domain := flag.String("d", "", "domain of the proxy account (for NTLM auth)")
	username := flag.String("u", whoAmI(), "username of the proxy account (for NTLM auth)")
	printHash := flag.Bool("H", false, "print hashed NTLM credentials for non-interactive use")
	version := flag.Bool("version", false, "print version number")
	flag.Parse()

	// default to localhost if no hosts are specified
	if len(hosts) == 0 {
		hosts = append(hosts, "localhost")
	}

	if *version {
		fmt.Println("Alpaca", BuildVersion)
		os.Exit(0)
	}

	var src credentialSource
	if *domain != "" {
		src = fromTerminal().forUser(*domain, *username)
	} else if value := os.Getenv("NTLM_CREDENTIALS"); value != "" {
		src = fromEnvVar(value)
	} else {
		src = fromKeyring()
	}

	var a *authenticator
	if src != nil {
		var err error
		a, err = src.getCredentials()
		if err != nil {
			log.Printf("Credentials not found, disabling proxy auth: %v", err)
		}
	}

	if *printHash {
		if a == nil {
			fmt.Println("Please specify a domain (using -d) and username (using -u)")
			os.Exit(1)
		}
		fmt.Printf("# Add this to your ~/.profile (or equivalent) and restart your shell\n")
		fmt.Printf("NTLM_CREDENTIALS=%q; export NTLM_CREDENTIALS\n", a)
		os.Exit(0)
	}

	errch := make(chan error)

	s := createServer(*port, *pacurl, a)
	for _, host := range hosts {
		address := net.JoinHostPort(host, strconv.Itoa(*port))
		for _, network := range networks(host) {
			go func(network string) {
				l, err := net.Listen(network, address)
				if err != nil {
					errch <- err
				} else {
					log.Printf("Listening on %s %s", network, address)
					errch <- s.Serve(l)
				}
			}(network)
		}
	}

	log.Fatal(<-errch)
}

func createServer(port int, pacurl string, a *authenticator) *http.Server {
	pacWrapper := NewPACWrapper(PACData{Port: port})
	proxyFinder := NewProxyFinder(pacurl, pacWrapper)
	proxyHandler := NewProxyHandler(a, getProxyFromContext, proxyFinder.blockProxy)
	mux := http.NewServeMux()
	pacWrapper.SetupHandlers(mux)

	// build the handler by wrapping middleware upon middleware
	var handler http.Handler = mux
	handler = RequestLogger(handler)
	handler = proxyHandler.WrapHandler(handler)
	handler = proxyFinder.WrapHandler(handler)
	handler = AddContextID(handler)

	return &http.Server{
		Handler: handler,
		// TODO: Implement HTTP/2 support. In the meantime, set TLSNextProto to a non-nil
		// value to disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
}

func networks(hostname string) []string {
	if hostname == "" {
		return []string{"tcp"}
	}
	addrs, err := net.LookupIP(hostname)
	if err != nil {
		log.Fatal(err)
	}
	nets := make([]string, 0, 2)
	ipv4 := false
	ipv6 := false
	for _, addr := range addrs {
		// addr == net.IPv4len doesn't work because all addrs use IPv6 format.
		if addr.To4() != nil {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	if ipv4 {
		nets = append(nets, "tcp4")
	}
	if ipv6 {
		nets = append(nets, "tcp6")
	}
	return nets
}
