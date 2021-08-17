// Copyright 2019, 2020, 2021 The Alpaca Authors
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
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/keybase/go-keychain"
)

const keyringSupported = true

type keyring struct {
	testKeychain *keychain.Keychain
	execCommand  func(name string, arg ...string) *exec.Cmd
}

func fromKeyring() *keyring {
	return &keyring{testKeychain: nil, execCommand: exec.Command}
}

func (k *keyring) readDefaultForNoMAD(key string) (string, error) {
	userDomain := "com.trusourcelabs.NoMAD"
	mpDomain := fmt.Sprintf("/Library/Managed Preferences/%s.plist", userDomain)

	// Read from managed preferences first
	out, err := k.execCommand("defaults", "read", mpDomain, key).Output()
	if err != nil {
		// Read from user preferences if not in managed preferences
		out, err = k.execCommand("defaults", "read", userDomain, key).Output()
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (k *keyring) readPasswordFromKeychain(userPrincipal string) string {
	// https://nomad.menu/help/keychain-usage/
	query := keychain.NewItem()
	if k.testKeychain != nil {
		query.SetMatchSearchList(*k.testKeychain)
	}
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetAccount(userPrincipal)
	query.SetReturnAttributes(true)
	query.SetReturnData(true)
	results, err := keychain.QueryItem(query)
	if err != nil || len(results) != 1 || results[0].Label != "NoMAD" {
		return ""
	}
	return string(results[0].Data)
}

func (k *keyring) getCredentials(ntlm, krb5 bool, krb5conf, kdc string) (credentials, error) {
	useKeychain, err := k.readDefaultForNoMAD("UseKeychain")
	if err != nil {
		return credentials{}, err
	} else if useKeychain != "1" {
		return credentials{}, errors.New("NoMAD found, but not configured to use keychain")
	}
	userPrincipal, err := k.readDefaultForNoMAD("UserPrincipal")
	if err != nil {
		return credentials{}, err
	}
	substrs := strings.Split(userPrincipal, "@")
	if len(substrs) != 2 {
		return credentials{}, errors.New(
			"Couldn't retrieve AD domain and username from NoMAD.")
	}
	user, domain := substrs[0], substrs[1]
	password := k.readPasswordFromKeychain(userPrincipal)
	log.Printf("Found NoMAD credentials for %s\\%s in system keychain", domain, user)
	var creds credentials
	if ntlm {
		creds.ntlm = ntlmcredFromPassword(domain, user, []byte(password))
	}
	if krb5 {
		creds.krb5 = krb5credFromPassword(user, domain, password, krb5conf, kdc)
	}
	return creds, nil
}
