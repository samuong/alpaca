// Copyright 2019,2020 The Alpaca Authors
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

var testKeychain *keychain.Keychain
var execCommand = exec.Command

func init() {
	getCredentialsFromKeyring = getCredentialsFromNoMAD
}

func readDefaultForNoMAD(key string) (string, error) {
	userDomain := "com.trusourcelabs.NoMAD"
	mpDomain := fmt.Sprintf("/Library/Managed Preferences/%s.plist", userDomain)

	// Read from managed preferences first
	out, err := execCommand("defaults", "read", mpDomain, key).Output()
	if err != nil {
		// Read from user preferences if not in managed preferences
		out, err = execCommand("defaults", "read", userDomain, key).Output()
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func readPasswordFromKeychain(userPrincipal string) string {
	// https://nomad.menu/help/keychain-usage/
	query := keychain.NewItem()
	if testKeychain != nil {
		query.SetMatchSearchList(*testKeychain)
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

func getCredentialsFromNoMAD() (*authenticator, error) {
	useKeychain, err := readDefaultForNoMAD("UseKeychain")
	if err != nil {
		return nil, err
	} else if useKeychain != "1" {
		return nil, errors.New("NoMAD found, but not configured to use keychain")
	}
	userPrincipal, err := readDefaultForNoMAD("UserPrincipal")
	if err != nil {
		return nil, err
	}
	substrs := strings.Split(userPrincipal, "@")
	if len(substrs) != 2 {
		return nil, errors.New("Couldn't retrieve AD domain and username from NoMAD.")
	}
	user, domain := substrs[0], substrs[1]
	hash := getNtlmHash([]byte(readPasswordFromKeychain(userPrincipal)))
	log.Printf("Found NoMAD credentails for %s\\%s in system keychain", domain, user)
	return &authenticator{domain, user, hash}, nil
}
