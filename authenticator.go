// Copyright 2019, 2021, 2024 The Alpaca Authors
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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/samuong/go-ntlmssp"
)

type authenticator struct {
	domain   string
	username string
	hash     []byte
}

func (a authenticator) scheme() string { return "NTLM" }

// safeWithoutChallenge reports true: NTLM Type 1 contains a workstation
// name and domain hint but no credential material, so it is safe to
// send before the proxy has explicitly advertised NTLM.
func (a authenticator) safeWithoutChallenge() bool { return true }

// applicableTo always returns true: NTLM has no host policy.
func (a authenticator) applicableTo(string) bool { return true }

func (a authenticator) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	hostname, _ := os.Hostname() // in case of error, just use the zero value ("") as hostname
	negotiate, err := ntlmssp.NewNegotiateMessage(a.domain, hostname)
	if err != nil {
		log.Printf("Error creating NTLM Type 1 (Negotiate) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization", "NTLM "+base64.StdEncoding.EncodeToString(negotiate))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		log.Printf("Error sending NTLM Type 1 (Negotiate) request: %v", err)
		return nil, err
	} else if resp.StatusCode != http.StatusProxyAuthRequired {
		log.Printf("Expected response with status 407, got %s", resp.Status)
		return resp, nil
	}
	_ = resp.Body.Close()
	encoded := findNTLMChallenge(resp.Header)
	if encoded == "" {
		log.Printf("NTLM Type 2 (Challenge) message not found in Proxy-Authenticate")
		return nil, fmt.Errorf("missing NTLM challenge in proxy response")
	}
	challenge, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Printf("Error decoding NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	authenticate, err := ntlmssp.ProcessChallengeWithHash(
		challenge, a.domain, a.username, a.hash)
	if err != nil {
		log.Printf("Error processing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization",
		"NTLM "+base64.StdEncoding.EncodeToString(authenticate))
	return rt.RoundTrip(req)
}

// findNTLMChallenge scans every Proxy-Authenticate header value for an
// NTLM challenge and returns the base64-encoded Type 2 message.
//
// A multi-auth proxy advertises several schemes in its 407 response,
// e.g. `Proxy-Authenticate: Negotiate`, `Proxy-Authenticate: NTLM`,
// `Proxy-Authenticate: Basic realm="proxy"`. After alpaca sends NTLM
// Type 1, the proxy replies with a 407 whose Proxy-Authenticate
// headers ALSO contain the NTLM Type 2 challenge — but the order is
// up to the server, so a simple Header.Get() (which returns the first
// value) can mis-parse if Negotiate or Basic appears first.
//
// RFC 7235 §4.3 also allows multiple challenges in a single comma-
// separated header value, but the NTLM challenge always appears as a
// dedicated value because it carries an opaque base64 token68. We
// match case-insensitively on the leading "NTLM " prefix and return
// only the token portion (no leading space).
func findNTLMChallenge(header http.Header) string {
	for _, value := range header.Values("Proxy-Authenticate") {
		trimmed := strings.TrimSpace(value)
		// "NTLM " prefix is case-insensitive per RFC 7235 §2.1
		// ("uses a case-insensitive token as a means to identify the
		// authentication scheme"). Use EqualFold for the prefix
		// check so e.g. "ntlm <token>" still matches.
		if len(trimmed) >= 5 && strings.EqualFold(trimmed[:5], "NTLM ") {
			return strings.TrimSpace(trimmed[5:])
		}
		// A bare "NTLM" with no challenge means the proxy is
		// re-advertising the scheme without a Type 2 token; that's
		// not a challenge we can parse, but it's also not a match,
		// so keep scanning.
	}
	return ""
}

func (a authenticator) String() string {
	return fmt.Sprintf("%s@%s:%s", a.username, a.domain, hex.EncodeToString(a.hash))
}
