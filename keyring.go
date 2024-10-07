// Copyright 2024 The Alpaca Authors
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

//go:build !darwin

package main

import (
	"fmt"
	"os"

	"github.com/samuong/go-ntlmssp"
	ring "github.com/zalando/go-keyring"
)

type keyring struct{}

func fromKeyring() *keyring {
	return &keyring{}
}

func (k *keyring) getCredentials() (*authenticator, error) {

	var (
		username = os.Getenv("NTLM_USERNAME")
		domain   = os.Getenv("NTLM_DOMAIN")
	)

	pwd, err := ring.Get("alpaca", username)
	if err != nil {
		return nil, fmt.Errorf("cannot get user secret from keyring: %w", err)
	}
	hash := ntlmssp.GetNtlmHash(pwd)
	return &authenticator{domain, username, hash}, nil
}
