package main

import (
	"errors"
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
	cmd := execCommand("defaults", "read", "com.trusourcelabs.NoMAD", key)
	out, err := cmd.Output()
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
	password := readPasswordFromKeychain(userPrincipal)
	log.Printf("Found NoMAD credentails for %s\\%s in system keychain", domain, user)
	return &authenticator{domain, user, password}, nil
}
