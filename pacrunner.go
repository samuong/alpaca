// Copyright 2019, 2021, 2023, 2024 The Alpaca Authors
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
	"encoding/binary"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/glob"
	"github.com/robertkrimen/otto"
)

// https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file

type PACRunner struct {
	vm *otto.Otto
	sync.Mutex
}

func (pr *PACRunner) Update(pacjs []byte) error {
	vm := otto.New()
	var err error
	set := func(name string, handler func(otto.FunctionCall) otto.Value) {
		if err != nil {
			return
		}
		err = vm.Set(name, handler)
	}
	set("isPlainHostName", isPlainHostName)
	set("dnsDomainIs", dnsDomainIs)
	set("localHostOrDomainIs", localHostOrDomainIs)
	set("isResolvable", isResolvable)
	set("isInNet", isInNet)
	set("dnsResolve", dnsResolve)
	set("convert_addr", convertAddr)
	set("myIpAddress", myIpAddress)
	set("dnsDomainLevels", dnsDomainLevels)
	set("shExpMatch", shExpMatch)
	set("weekdayRange", func(fc otto.FunctionCall) otto.Value {
		return weekdayRange(fc, time.Now())
	})
	set("dateRange", func(fc otto.FunctionCall) otto.Value {
		return dateRange(fc, time.Now())
	})
	set("timeRange", func(fc otto.FunctionCall) otto.Value {
		return timeRange(fc, time.Now())
	})
	if err != nil {
		return err
	}
	_, err = vm.Run(pacjs)
	if err != nil {
		return err
	}
	pr.vm = vm
	return nil
}

func (pr *PACRunner) FindProxyForURL(u url.URL) (string, error) {
	pr.Lock()
	defer pr.Unlock()
	if u.Scheme == "" {
		// When a net/http Server parses a CONNECT request, the URL will
		// have no Scheme. In that case, assume the scheme is "https".
		u.Scheme = "https"
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
		// Strip the path and query components of https:// URLs.
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file#Parameters
		// Like Chrome, also strip the path and query for wss:// URLs (secure WebSockets).
		// https://cs.chromium.org/chromium/src/net/proxy_resolution/proxy_resolution_service.cc?rcl=fba6691ffca770dd0c916418601b9c9c019a2929&l=383
		// It also seems like a good idea to strip the fragment, so do that too.
		u.Path = "/"
		u.RawPath = "/"
		u.RawQuery = ""
		u.Fragment = ""
	}
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

func localHostOrDomainIs(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	hostdom := call.Argument(1).String()
	return toValue(host == hostdom || strings.HasPrefix(hostdom, host+"."))
}

func isResolvable(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	_, err := net.LookupHost(host)
	return toValue(err == nil)
}

func isInNet(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	pattern := call.Argument(1).String()
	mask := call.Argument(2).String()
	buf := net.ParseIP(mask).To4()
	if len(buf) != 4 {
		return toValue(false)
	}

	m := net.IPv4Mask(buf[0], buf[1], buf[2], buf[3])
	maskedIP := resolve(host).Mask(m)
	maskedPattern := net.ParseIP(pattern).To4().Mask(m)
	return toValue(maskedIP.Equal(maskedPattern))
}

func dnsResolve(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	return toValue(resolve(host).String())
}

func resolve(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		// The given host is already an IP(v4) address; just return it.
		return ip.To4()
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		// There might be multiple IP addresses for this host. Return the first IPv4 address
		// that we can find.
		if ipv4 := net.ParseIP(addr).To4(); ipv4 != nil {
			return ipv4
		}
	}
	return nil
}

func convertAddr(call otto.FunctionCall) otto.Value {
	ipaddr := call.Argument(0).String()
	ipv4 := net.ParseIP(ipaddr).To4()
	if ipv4 == nil {
		return toValue(0)
	}
	return toValue(binary.BigEndian.Uint32(ipv4))
}

func myIpAddress(call otto.FunctionCall) otto.Value {
	// https://chromium.googlesource.com/chromium/src/+/ee43fa5328856129f46566b2ea1be5811739681c/net/docs/proxy.md#Resolving-client_s-IP-address-within-a-PAC-script-using-myIpAddress
	public := []string{"8.8.8.8", "2001:4860:4860::8888"}
	for _, remoteAddr := range public {
		if localAddr := probeRoute(remoteAddr); localAddr != "" {
			return toValue(localAddr)
		}
	}
	if ip := resolveHostname(); ip != "" {
		return toValue(ip)
	}
	private := []string{"10.0.0.0", "172.16.0.0", "192.168.0.0", "FC00::"}
	for _, remoteAddr := range private {
		if localAddr := probeRoute(remoteAddr); localAddr != "" {
			return toValue(localAddr)
		}
	}
	return toValue("127.0.0.1")
}

// probeRoute creates a UDP "connection" to the remote address, and returns the
// local interface address. This does involve a system call, but does not
// generate any network traffic since UDP is a connectionless protocol.
func probeRoute(remote string) string {
	conn, err := net.Dial("udp", net.JoinHostPort(remote, "80"))
	if err != nil {
		return ""
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		// XXX: This is very unexpected, is it better to panic here?
		return ""
	}
	if local.IP.IsLoopback() ||
		local.IP.IsLinkLocalUnicast() ||
		local.IP.IsLinkLocalMulticast() {
		return ""
	}
	return local.IP.String()
}

// resolveHostname does a DNS resolve of the machine's hostname, and returns
// the first IPv4 result if there is one, or the first IPv6 address, or the
// empty string.
func resolveHostname() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	var ipv6 net.IP
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			continue
		}
		if ipv4 := ip.To4(); len(ipv4) == net.IPv4len {
			return ipv4.String()
		} else if len(ip) == net.IPv6len && ipv6 == nil {
			// We explicitly favour IPv4 over IPv6, so only return
			// an IPv6 address if we can't find an IPv4 one.
			ipv6 = ip
		}
	}
	if ipv6 == nil {
		return ""
	}
	return ipv6.String()
}

func dnsDomainLevels(call otto.FunctionCall) otto.Value {
	host := call.Argument(0).String()
	return toValue(strings.Count(host, "."))
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

func weekdayRange(call otto.FunctionCall, now time.Time) otto.Value {
	if call.Argument(len(call.ArgumentList)-1).String() == "GMT" {
		now = now.In(time.UTC)
	}
	weekdays := map[string]time.Weekday{
		"SUN": time.Sunday, "MON": time.Monday, "TUE": time.Tuesday, "WED": time.Wednesday,
		"THU": time.Thursday, "FRI": time.Friday, "SAT": time.Saturday,
	}
	wd1, ok := weekdays[call.Argument(0).String()]
	if !ok {
		return otto.UndefinedValue()
	}
	wd2, ok := weekdays[call.Argument(1).String()]
	if !ok {
		return toValue(now.Weekday() == wd1)
	} else if wd1 <= wd2 {
		return toValue(wd1 <= now.Weekday() && now.Weekday() <= wd2)
	} else {
		return toValue(wd1 == now.Weekday() || wd2 == now.Weekday())
	}
}

func dateRange(call otto.FunctionCall, now time.Time) otto.Value {
	argc := len(call.ArgumentList)
	if call.Argument(argc-1).String() == "GMT" {
		now = now.In(time.UTC)
		argc--
	}

	var days []int
	var months []time.Month
	var years []int

	monthmap := map[string]time.Month{
		"JAN": time.January, "FEB": time.February, "MAR": time.March,
		"APR": time.April, "MAY": time.May, "JUN": time.June,
		"JUL": time.July, "AUG": time.August, "SEP": time.September,
		"OCT": time.October, "NOV": time.November, "DEC": time.December,
	}

	for i := 0; i < argc; i++ {
		if call.Argument(i).IsNumber() {
			n, err := call.Argument(i).ToInteger()
			if err != nil {
				return otto.UndefinedValue()
			} else if 1 <= n && n <= 31 {
				days = append(days, int(n))
			} else {
				years = append(years, int(n))
			}
		} else if month, ok := monthmap[call.Argument(i).String()]; ok {
			months = append(months, month)
		} else {
			return otto.UndefinedValue()
		}
	}

	switch max(len(days), len(months), len(years)) {
	case 1:
		// One (possibly partial) date provided; match it against the current date.
		if len(days) == 1 && days[0] != now.Day() {
			return otto.FalseValue()
		} else if len(months) == 1 && months[0] != now.Month() {
			return otto.FalseValue()
		} else if len(years) == 1 && years[0] != now.Year() {
			return otto.FalseValue()
		} else {
			return otto.TrueValue()
		}
	case 2:
		// Two dates provided; check that the current date is inside the range.
		y1, m1, d1 := now.Date()
		y2, m2, d2 := now.Date()
		if len(days) == 2 {
			d1, d2 = days[0], days[1]
		}
		if len(months) == 2 {
			m1, m2 = months[0], months[1]
		}
		if len(years) == 2 {
			y1, y2 = years[0], years[1]
		}
		h, m, s := now.Clock()
		ns, loc := now.Nanosecond(), now.Location()
		start := time.Date(y1, m1, d1, h, m, s, ns, loc)
		end := time.Date(y2, m2, d2, h, m, s, ns, loc)
		return toValue(!start.After(now) && !end.Before(now))
	default:
		// Zero, three or more dates provided. Something's wrong.
		return otto.UndefinedValue()
	}
}

func max(a, b, c int) int {
	if a >= b && a >= c {
		return a
	} else if b >= c {
		return b
	} else {
		return c
	}
}

func timeRange(call otto.FunctionCall, now time.Time) otto.Value {
	argc := len(call.ArgumentList)
	if call.Argument(argc-1).String() == "GMT" {
		now = now.In(time.UTC)
		argc--
	}
	h1, m1, s1, h2, m2, s2 := 0, 0, 0, 0, 0, 0
	var err error
	toInt := func(idx int) int {
		val, err2 := call.Argument(idx).ToInteger()
		if err2 != nil {
			err = err2
		}
		return int(val)
	}
	switch argc {
	case 1:
		h1 = toInt(0)
		h2 = h1 + 1
	case 2:
		h1 = toInt(0)
		h2 = toInt(1)
	case 4:
		h1, m1 = toInt(0), toInt(1)
		h2, m2 = toInt(2), toInt(3)
	case 6:
		h1, m1, s1 = toInt(0), toInt(1), toInt(2)
		h2, m2, s2 = toInt(3), toInt(4), toInt(5)
	default:
		return otto.UndefinedValue()
	}
	if err != nil {
		return otto.UndefinedValue()
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), h1, m1, s1, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), h2, m2, s2, 0, now.Location())
	return toValue(!start.After(now) && end.After(now))
}
