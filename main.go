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
	"io"
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
	username := flag.String("u", whoAmI(), "username for proxy auth (NTLM)")
	printHash := flag.Bool("H", false, "print hashed NTLM credentials for non-interactive use")
	kerberosWait := flag.Int("w", 0,
		"seconds to wait for a Kerberos ticket at startup (macOS only)")
	noKerberos := flag.Bool("no-kerberos", false,
		"disable Kerberos/Negotiate auto-detection (macOS only)")
	quiet := flag.Bool("q", false, "quiet mode, suppress all log output")
	debug := flag.Bool("debug", false,
		"verbose troubleshooting log output. Adds DEBUG-prefixed "+
			"lines explaining which auth methods the picker "+
			"considered for each 407, the resolved SPN allowlist, "+
			"the SPN alpaca asked GSS for, and so on. Implies "+
			"-q false.")
	version := flag.Bool("version", false, "print version number")
	enableSocks := flag.Bool("enable-socks", false, "allow SOCKS5 proxies from PAC files")
	// -k is retained for backward compatibility: it sets the default
	// Kerberos wait time to 30s if -w is not also specified.
	kerberos := flag.Bool("k", false,
		"deprecated: enable Kerberos wait at startup (macOS only). "+
			"Negotiate is auto-detected when a ticket is present; pass -w to wait.")
	flag.Parse()

	if *quiet {
		log.SetOutput(io.Discard)
	}
	debugEnabled = *debug

	// default to localhost if no hosts are specified
	if len(hosts) == 0 {
		hosts = append(hosts, "localhost")
	}

	if *version {
		fmt.Println("Alpaca", BuildVersion)
		os.Exit(0)
	}

	var basicAuth *basicAuthenticator
	var a *authenticator

	// Basic credentials come from BASIC_CREDENTIALS to avoid leaking the
	// password into shell history (mirrors how NTLM_CREDENTIALS works).
	if value := os.Getenv("BASIC_CREDENTIALS"); value != "" {
		basicAuth = newBasicAuthenticator(value)
		log.Println("Basic proxy authentication configured from BASIC_CREDENTIALS")
	}

	// NTLM credential sources
	var src credentialSource
	if *domain != "" {
		src = fromTerminal().forUser(*domain, *username)
	} else if value := os.Getenv("NTLM_CREDENTIALS"); value != "" {
		src = fromEnvVar(value)
	} else {
		src = fromKeyring()
	}
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

	// Build auth chain: Negotiate → NTLM → Basic (matches Chrome's hierarchy;
	// Basic has the lowest security score because it sends credentials unencrypted).
	//
	// Kerberos/Negotiate is auto-detected on macOS: if a valid ticket is
	// present at startup (or appears within -w seconds), Negotiate is
	// added to the chain. No flag needed for the common "Apple SSO is
	// signed in" case — alpaca behaves like the keyring source.
	var methods []proxyAuthenticator
	if !*noKerberos {
		wait := *kerberosWait
		if *kerberos && wait == 0 {
			// Backward compat for the legacy -k flag.
			wait = 30
			fmt.Fprintln(os.Stderr,
				"Note: -k is deprecated; pass -w 30 to retain the "+
					"previous startup-wait behaviour. Negotiate is "+
					"auto-detected without -k whenever a Kerberos "+
					"ticket is present.")
		}
		if neg := newNegotiateAuthenticator(wait); neg != nil {
			log.Println("Kerberos/Negotiate authentication available")
			methods = append(methods, neg)
		}
	}
	if a != nil {
		methods = append(methods, a)
	}
	if basicAuth != nil {
		methods = append(methods, basicAuth)
	}
	auth := newAuthChain(methods...)
	if auth == nil {
		log.Println("No authentication methods configured; alpaca will " +
			"surface proxy 407 responses as 502 Bad Gateway to clients")
	} else if debugEnabled {
		names := make([]string, 0, len(methods))
		for _, m := range methods {
			names = append(names, m.scheme())
		}
		debugf("Auth chain configured (preference order): %v", names)
	}

	errch := make(chan error)

	s := createServer(*port, *pacurl, auth, *enableSocks)
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

func createServer(port int, pacurl string, auth *authChain, enableSocks bool) *http.Server {
	pacWrapper := NewPACWrapper(PACData{Port: port})
	proxyFinder := NewProxyFinder(pacurl, pacWrapper, enableSocks)
	proxyHandler := NewProxyHandler(auth, getProxyFromContext, proxyFinder.blockProxy)
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
