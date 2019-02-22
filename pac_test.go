package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
)

func newProxyFinder(t *testing.T, expr string) *ProxyFinder {
	pf, err := NewProxyFinder(strings.NewReader(fmt.Sprintf(
		"function FindProxyForURL(url, host) { return %s }", expr)))
	require.Nil(t, err)
	return pf
}

func check(t *testing.T, pf *ProxyFinder, addr, expected string) {
	u, err := url.Parse(addr)
	require.Nil(t, err)
	proxy, err := pf.FindProxyForURL(u)
	require.Nil(t, err)
	assert.Equal(t, expected, proxy)
}

func TestDirect(t *testing.T) {
	pf := newProxyFinder(t, `"DIRECT"`)
	check(t, pf, "https://anz.com", "DIRECT")
}

func TestIsPlainHostName(t *testing.T) {
	pf := newProxyFinder(t, `isPlainHostName(host) ? "y" : "n"`)
	check(t, pf, "https://www", "y")
	check(t, pf, "https://anz.com", "n")
}

func TestDnsDomainIs(t *testing.T) {
	pf := newProxyFinder(t, `dnsDomainIs(host, ".anz.com") ? "y" : "n"`)
	check(t, pf, "https://www.anz.com", "y")
	check(t, pf, "https://www", "n")
}

func TestDnsDomainIsAnySuffix(t *testing.T) {
	// See https://bugs.chromium.org/p/chromium/issues/detail?id=299649.
	pf := newProxyFinder(t, `dnsDomainIs(host, "anz.com") ? "y" : "n"`)
	check(t, pf, "https://notanz.com", "y")
}

func TestShExpMatch(t *testing.T) {
	pf := newProxyFinder(t, `shExpMatch(url, "*/b/*") ? "y" : "n"`)
	check(t, pf, "http://anz.com/a/b/c.html", "y")
	check(t, pf, "http://anz.com/d/e/f.html", "n")
}
