package main

import (
	"errors"
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

func getCredentialsFromNoMAD() (authenticator, error) {
	useKeychain, err := readDefaultForNoMAD("UseKeychain")
	if err != nil {
		return authenticator{}, err
	} else if useKeychain != "1" {
		return authenticator{}, errors.New(`NoMAD found, but UseKeychain != 1. To sync your AD password to the system keychain (and have Alpaca automatically retrieve it from there) open NoMAD's Preferences dialog and check "Use Keychain".`)
	}
	userPrincipal, err := readDefaultForNoMAD("UserPrincipal")
	if err != nil {
		return authenticator{}, err
	}
	substrs := strings.Split(userPrincipal, "@")
	if len(substrs) != 2 {
		return authenticator{}, errors.New("Couldn't retrieve AD domain and username from NoMAD.")
	}
	user, domain := substrs[0], substrs[1]
	password := readPasswordFromKeychain(userPrincipal)
	return authenticator{domain, user, password}, nil
}
