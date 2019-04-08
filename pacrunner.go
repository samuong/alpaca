package main

import (
	"errors"
	"github.com/gobwas/glob"
	"github.com/robertkrimen/otto"
	"io"
	"net/url"
	"strings"
	"sync"
)

// https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file

type PacRunner struct {
	vm  *otto.Otto
	mux sync.Mutex
}

func NewPacRunner(r io.Reader) (*PacRunner, error) {
	vm := otto.New()
	var err error
	set := func(name string, handler func(otto.FunctionCall) otto.Value) {
		if err != nil {
			return
		}
		err = vm.Set(name, handler)
	}
	// TODO: These three functions are the only ones that are used by the
	// ANZ PAC file. Implement the rest later.
	set("isPlainHostName", isPlainHostName)
	set("dnsDomainIs", dnsDomainIs)
	set("shExpMatch", shExpMatch)
	if err != nil {
		return nil, err
	}
	_, err = vm.Run(r)
	if err != nil {
		return nil, err
	}
	return &PacRunner{vm: vm}, nil
}

func (pr *PacRunner) FindProxyForURL(u *url.URL) (string, error) {
	pr.mux.Lock()
	defer pr.mux.Unlock()
	// TODO: Strip the path and query components of https:// URLs.
	val, err := pr.vm.Call("FindProxyForURL", nil, u.String(), u.Hostname())
	if err != nil {
		return "", err
	} else if !val.IsString() {
		return "", errors.New("FindProxyForURL didn't return a string")
	}
	return val.String(), nil
}

func toValue(unwrapped interface{}) otto.Value {
	wrapped, err := otto.ToValue(unwrapped)
	if err != nil {
		return otto.UndefinedValue()
	} else {
		return wrapped
	}
}

func isPlainHostName(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	return toValue(!strings.ContainsRune(host, '.'))
}

func dnsDomainIs(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	domain := call.Argument(1).String()
	return toValue(strings.HasSuffix(host, domain))
}

func shExpMatch(call otto.FunctionCall) otto.Value {
	str := call.Argument(0).String()
	shexp := call.Argument(1).String()
	g, err := glob.Compile(shexp)
	if err != nil {
		return otto.UndefinedValue()
	}
	return toValue(g.Match(str))
}
