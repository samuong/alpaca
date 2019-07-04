package main

import (
	"github.com/keybase/go-keychain"
	"log"
	"os/exec"
	"strings"
)

func init() {
	getCredentialsFromKeyring = getCredentialsFromNoMAD
}

func readDefaultForNoMAD(key string) (string, bool) {
	cmd := exec.Command("defaults", "read", "com.trusourcelabs.NoMAD", key)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func readPasswordFromKeychain(userPrincipal string) string {
	// https://nomad.menu/help/keychain-usage/
	query := keychain.NewItem()
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

func getCredentialsFromNoMAD() (authenticator, bool) {
	useKeychain, ok := readDefaultForNoMAD("UseKeychain")
	if !ok {
		return authenticator{}, false
	} else if useKeychain != "1" {
		log.Println(`NoMAD is configured with UseKeychain != 1. To sync your AD password to the system keychain (and have Alpaca automatically retrieve it from there) open NoMAD's Preferences dialog and check "Use Keychain".`)
		return authenticator{}, false
	}
	userPrincipal, ok := readDefaultForNoMAD("UserPrincipal")
	if !ok {
		log.Println("Couldn't retrieve AD domain and username from NoMAD (UserPrincipal).")
		return authenticator{}, false
	}
	substrs := strings.Split(userPrincipal, "@")
	if len(substrs) != 2 {
		log.Printf("Invalid UserPrincipal %v\n", userPrincipal)
		return authenticator{}, false
	}
	user, domain := substrs[0], substrs[1]
	password := readPasswordFromKeychain(userPrincipal)
	return authenticator{domain, user, password}, true
}
