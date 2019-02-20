package main

import (
	"errors"
	"github.com/robertkrimen/otto"
	"io"
	"net/url"
	"strings"
)

// https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file

type ProxyFinder struct {
	vm *otto.Otto
}

func NewProxyFinder(r io.Reader) (*ProxyFinder, error) {
	vm := otto.New()
	var err error
	set := func(name string, handler func(otto.FunctionCall) otto.Value) {
		if err != nil {
			return
		}
		err = vm.Set(name, handler)
	}
	// TODO: These three functions are the only ones that are used by the
	// ANZ PAC file. Implement these first, then the rest later.
	set("isPlainHostName", isPlainHostName)
	//set("dnsDomainIs", dnsDomainIs)
	//set("shExpMatch", shExpMatch)
	if err != nil {
		return nil, err
	}
	vm.Run(r)
	return &ProxyFinder{vm}, nil
}

func (pf *ProxyFinder) FindProxyForURL(u *url.URL) (string, error) {
	// TODO: Strip the path and query components of https:// URLs.
	val, err := pf.vm.Call("FindProxyForURL", nil, u.String(), u.Hostname())
	if err != nil {
		return "", err
	} else if !val.IsString() {
		return "", errors.New("FindProxyForURL didn't return a string")
	}
	return val.String(), nil
}

func isPlainHostName(call otto.FunctionCall) otto.Value {
	arg := call.Argument(0)
	if arg.IsUndefined() {
		return otto.UndefinedValue()
	}
	host := arg.String()
	if !strings.ContainsRune(host, '.') {
		return otto.TrueValue()
	} else {
		return otto.FalseValue()
	}
}
