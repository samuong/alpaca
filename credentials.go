// Copyright 2021 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance from the License.
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"log"
	"os"
	"strings"

	"golang.org/x/term"
)

type credentialSource interface {
	getCredentials(bool, bool, string, string) (credentials, error)
}

type terminal struct {
	readPassword     func() ([]byte, error)
	stdout           io.Writer
	domain, username string
}

func fromTerminal() *terminal {
	return &terminal{
		readPassword: func() ([]byte, error) {
			return term.ReadPassword(int(os.Stdin.Fd()))
		},
		stdout: os.Stdout,
	}
}

func (t *terminal) forUser(domain, username string) *terminal {
	t.domain = domain
	t.username = username
	return t
}

func (t *terminal) getCredentials(ntlm, krb5 bool, krb5conf, kdc string) (credentials, error) {
	fmt.Fprintf(t.stdout, "Password (for %s\\%s): ", t.domain, t.username)
	buf, err := t.readPassword()
	fmt.Println()
	if err != nil {
		return credentials{}, fmt.Errorf("error reading password from stdin: %w", err)
	}
	var creds credentials
	if ntlm {
		creds.ntlm = ntlmcredFromPassword(t.domain, t.username, buf)
	}
	if krb5 {
		creds.krb5 = krb5credFromPassword(t.username, t.domain, string(buf), krb5conf, kdc)
	}
	return creds, nil
}

type envVar struct {
	value string
}

func fromEnvVar(value string) *envVar {
	return &envVar{value: value}
}

func (e *envVar) getCredentials(ntlm, krb5 bool, krb5conf, kdc string) (credentials, error) {
	at := strings.IndexRune(e.value, '@')
	colon := strings.IndexRune(e.value, ':')
	if at == -1 || colon == -1 || at > colon {
		return credentials{}, errors.New(
			"invalid credentials string, please run `alpaca -H`")
	}
	domain := e.value[at+1 : colon]
	username := e.value[0:at]
	hash := e.value[colon+1:]
	log.Printf("Found credentials for %s\\%s in environment", domain, username)
	return credentials{ntlm: ntlmcredFromHash(domain, username, hash), krb5: nil}, nil
}

type credentials struct {
	ntlm *ntlmcred
	krb5 *krb5cred
}

func (c credentials) choose(resp *http.Response) credential {
	ntlm, negotiate := false, false
	for _, value := range resp.Header.Values("Proxy-Authenticate") {
		if strings.HasPrefix(value, "Negotiate") {
			negotiate = true
		} else if strings.HasPrefix(value, "NTLM") {
			ntlm = true
		}
	}
	if negotiate {
		if c.krb5 != nil {
			return c.krb5
		}
		return c.ntlm
	} else if ntlm {
		return c.ntlm
	}
	return nil
}

type credential interface {
	wrap(http.RoundTripper) http.RoundTripper
}
