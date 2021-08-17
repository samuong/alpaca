package main

import (
	"net/http"
)

type krb5cred struct {
}

func (k *krb5cred) wrap(delegate http.RoundTripper) http.RoundTripper {
	return krb5auth{k, delegate}
}

func krb5credFromPassword(username, realm, password, krb5conf, kdc string) *krb5cred {
	return nil
}

type krb5auth struct {
	cred     *krb5cred
	delegate http.RoundTripper
}

func (k krb5auth) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}
