// Copyright 2019, 2021, 2024, 2025 The Alpaca Authors
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

	"github.com/ThomsonReutersEikon/go-ntlm/ntlm"
	"github.com/samuong/go-ntlmssp"
)

type authenticator struct {
	domain   string
	username string
	hash     []byte
}

func (a authenticator) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	hostname, _ := os.Hostname() // in case of error, just use the zero value ("") as hostname
	// XXX: github.com/ThomsonReutersEikon/go-ntlm doesn't seem to have a
	// way to generate a negotiate message (or even a hardcoded one in the
	// library?). Use the one from github.com/Azure/go-ntlmssp, and hope
	// that it works ¯\_(ツ)_/¯
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
	resp.Body.Close()
	challengeBytes, err := base64.StdEncoding.DecodeString(
		strings.TrimPrefix(resp.Header.Get("Proxy-Authenticate"), "NTLM "))
	if err != nil {
		log.Printf("Error decoding NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	challenge, err := ntlm.ParseChallengeMessage(challengeBytes)
	if err != nil {
		log.Printf("Error parsing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	session, err := ntlm.CreateClientSession(ntlm.Version2, ntlm.ConnectionlessMode)
	session.SetUserInfo(a.username, os.Getenv("ALPACA_PASSWORD"), a.domain)
	if err := session.ProcessChallengeMessage(challenge); err != nil {
		log.Printf("Error processing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	authenticate, err := session.GenerateAuthenticateMessage()
	if err != nil {
		log.Printf("Error processing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization",
		"NTLM "+base64.StdEncoding.EncodeToString(authenticate.Bytes()))
	return rt.RoundTrip(req)
}

func (a authenticator) String() string {
	return fmt.Sprintf("%s@%s:%s", a.username, a.domain, hex.EncodeToString(a.hash))
}
