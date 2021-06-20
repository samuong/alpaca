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
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"unicode/utf16"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"golang.org/x/crypto/md4" //nolint:staticcheck
)

type authenticator struct {
	domain, username, hash string
}

func (a authenticator) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	krb5conf := fmt.Sprintf(
		"[libdefaults]\ndefault_realm=%s\n[realms]\n%s={kdc=%s}\n",
		a.domain, a.domain, a.domain,
	)
	cfg, err := config.NewFromString(krb5conf)
	cl := client.NewWithPassword(a.username, a.domain, "asdf", cfg, client.DisablePAFXFAST(true))
	spn := "HTTP/..."
	s := spnego.SPNEGOClient(cl, spn)
	if err := s.AcquireCred(); err != nil {
		return nil, fmt.Errorf("could not acquire client credential: %v", err)
	}
	st, err := s.InitSecContext()
	if err != nil {
		return nil, fmt.Errorf("could not initialize context: %v", err)
	}
	nb, err := st.Marshal()
	if err != nil {
		return nil, fmt.Errorf("could not marshal SPNEGO: %v", err)
	}
	hs := "Negotiate " + base64.StdEncoding.EncodeToString(nb)
	req.Header.Set("Proxy-Authorization", hs)
	return rt.RoundTrip(req)
}

func (a authenticator) String() string {
	return fmt.Sprintf("%s@%s:%s", a.username, a.domain, a.hash)
}

// The following two functions are taken from "github.com/Azure/go-ntlmssp". This code was
// copyrighted (2016) by Microsoft and licensed under the MIT License:
// https://github.com/Azure/go-ntlmssp/blob/66371956d46c8e2133a2b72b3d320e435465011f/LICENSE.

// https://github.com/Azure/go-ntlmssp/blob/66371956d46c8e2133a2b72b3d320e435465011f/nlmp.go#L21-L25
func getNtlmHash(password []byte) string {
	hash := md4.New()
	hash.Write(toUnicode(string(password)))
	return hex.EncodeToString(hash.Sum(nil))
}

// https://github.com/Azure/go-ntlmssp/blob/66371956d46c8e2133a2b72b3d320e435465011f/unicode.go#L24-L29
func toUnicode(s string) []byte {
	uints := utf16.Encode([]rune(s))
	b := bytes.Buffer{}
	binary.Write(&b, binary.LittleEndian, &uints) //nolint:errcheck
	return b.Bytes()
}
