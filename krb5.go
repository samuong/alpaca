package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

type krb5cred struct {
	cl *client.Client
}

func krb5credFromPassword(username, realm, password, krb5conf, kdc string) *krb5cred {
	var cfg *config.Config
	if krb5conf != "" {
		f, err := os.Open(krb5conf)
		if err != nil {
			// TODO: need to return an error too?
			return nil
		}
		cfg, err = config.NewFromReader(f)
		if err != nil {
			// TODO: need to return an error too?
			return nil
		}
	} else {
		cfg = config.New()
		cfg.LibDefaults.DefaultRealm = realm
		if kdc != "" {
			cfg.Realms = []config.Realm{{Realm: realm, KDC: []string{kdc}}}
		} else {
			cfg.Realms = []config.Realm{{Realm: realm, KDC: []string{realm}}}
		}
	}
	// https://github.com/jcmturner/gokrb5/blob/663478bf457f1fc3275973bea5b7b787cd332015/USAGE.md#active-directory-kdc-and-fast-negotiation
	cl := client.NewWithPassword(username, realm, password, cfg, client.DisablePAFXFAST(true))
	return &krb5cred{cl: cl}
}

func (k *krb5cred) wrap(delegate http.RoundTripper, proxy string) http.RoundTripper {
	return krb5auth{spnego.SPNEGOClient(k.cl, "HTTP/"+proxy), delegate}
}

type krb5auth struct {
	spnegoClient *spnego.SPNEGO
	delegate     http.RoundTripper
}

func (k krb5auth) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := k.spnegoClient.AcquireCred(); err != nil {
		return nil, fmt.Errorf("could not acquire client credential: %v", err)
	}
	st, err := k.spnegoClient.InitSecContext()
	if err != nil {
		return nil, fmt.Errorf("could not initialize context: %v", err)
	}
	nb, err := st.Marshal()
	if err != nil {
		return nil, fmt.Errorf("could not marshal SPNEGO: %v", err)
	}
	hs := "Negotiate " + base64.StdEncoding.EncodeToString(nb)
	req.Header.Set("Proxy-Authorization", hs)
	return nil, nil
}
