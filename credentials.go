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
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/samuong/go-ntlmssp"
	"golang.org/x/term"
)

type credentialSource interface {
	getCredentials() (*authenticator, error)
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

func (t *terminal) getCredentials() (*authenticator, error) {
	fmt.Fprintf(t.stdout, "Password (for %s\\%s): ", t.domain, t.username)
	buf, err := t.readPassword()
	fmt.Println()
	if err != nil {
		return nil, fmt.Errorf("error reading password from stdin: %w", err)
	}
	return &authenticator{
		domain:   t.domain,
		username: t.username,
		hash:     ntlmssp.GetNtlmHash(string(buf)),
	}, nil
}

type envVar struct {
	value string
}

func fromEnvVar(value string) *envVar {
	return &envVar{value: value}
}

func (e *envVar) getCredentials() (*authenticator, error) {
	at := strings.IndexRune(e.value, '@')
	colon := strings.IndexRune(e.value, ':')
	if at == -1 || colon == -1 || at > colon {
		return nil, errors.New("invalid credentials string, please run `alpaca -H`")
	}
	domain := e.value[at+1 : colon]
	username := e.value[0:at]
	hash, err := hex.DecodeString(e.value[colon+1:])
	if err != nil {
		return nil, fmt.Errorf("invalid hash, please run `alpaca -H`: %w", err)
	}
	log.Printf("Found credentials for %s\\%s in environment", domain, username)
	return &authenticator{domain, username, hash}, nil
}
