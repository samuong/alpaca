// Copyright 2019, 2021, 2023, 2024, 2025 The Alpaca Authors
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
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/gobwas/glob"
)

// https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file

var pacVM *goja.Runtime

type PACRunner struct {
	vm    *goja.Runtime
	mutex sync.Mutex
}

func (pr *PACRunner) Update(pacjs []byte) error {
	vm := goja.New()
	var err error
	set := func(name string, handler func(goja.FunctionCall) goja.Value) {
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
	set("myIpAddressEx", myIpAddressEx)
	set("dnsDomainLevels", dnsDomainLevels)
	set("shExpMatch", shExpMatch)
	set("weekdayRange", func(fc goja.FunctionCall) goja.Value {
		return weekdayRange(fc, time.Now())
	})
	set("dateRange", func(fc goja.FunctionCall) goja.Value {
		return dateRange(fc, time.Now())
	})
	set("timeRange", func(fc goja.FunctionCall) goja.Value {
		return timeRange(fc, time.Now())
	})
	if err != nil {
		return err
	}
	_, err = vm.RunString(string(pacjs))
	if err != nil {
		return err
	}
	pr.mutex.Lock()
	pr.vm = vm
	pacVM = vm
	pr.mutex.Unlock()
	return nil
}

func (pr *PACRunner) FindProxyForURL(u url.URL) (string, error) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
		u.Path = "/"
		u.RawPath = "/"
		u.RawQuery = ""
		u.Fragment = ""
	}
	val, err := pr.vm.RunString("FindProxyForURL(" + fmt.Sprintf("%q", u.String()) + ", " + fmt.Sprintf("%q", u.Hostname()) + ")")
	if err != nil {
		return "", err
	}
	return val.Export().(string), nil
}

func toValue(unwrapped interface{}) goja.Value {
	return pacVM.ToValue(unwrapped)
}

func isPlainHostName(call goja.FunctionCall) goja.Value {
	host := call.Argument(0).String()
	return toValue(!strings.ContainsRune(host, '.'))
}

func dnsDomainIs(call goja.FunctionCall) goja.Value {
	host := call.Argument(0).String()
	domain := call.Argument(1).String()
	return toValue(strings.HasSuffix(host, domain))
}

func localHostOrDomainIs(call goja.FunctionCall) goja.Value {
	host := call.Argument(0).String()
	hostdom := call.Argument(1).String()
	return toValue(host == hostdom || strings.HasPrefix(hostdom, host+"."))
}

func isResolvable(call goja.FunctionCall) goja.Value {
	host := call.Argument(0).String()
	_, err := net.LookupHost(host)
	return toValue(err == nil)
}

func isInNet(call goja.FunctionCall) goja.Value {
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

func dnsResolve(call goja.FunctionCall) goja.Value {
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

func convertAddr(call goja.FunctionCall) goja.Value {
	ipaddr := call.Argument(0).String()
	ipv4 := net.ParseIP(ipaddr).To4()
	if ipv4 == nil {
		return toValue(0)
	}
	return toValue(binary.BigEndian.Uint32(ipv4))
}

func myIpAddress(call goja.FunctionCall) goja.Value {
	if localAddr := probeRoute("8.8.8.8"); localAddr != "" {
		return toValue(localAddr)
	}
	if ips := resolveHostname(false); len(ips) > 0 {
		return toValue(ips[0].String())
	}
	private := []string{"10.0.0.0", "172.16.0.0", "192.168.0.0"}
	for _, remoteAddr := range private {
		if localAddr := probeRoute(remoteAddr); localAddr != "" {
			return toValue(localAddr)
		}
	}
	return toValue("127.0.0.1")
}

func myIpAddressEx(call goja.FunctionCall) goja.Value {
	public := []string{"8.8.8.8", "2001:4860:4860::8888"}
	if ips := probeRoutes(public); ips != "" {
		return toValue(ips)
	}
	if ips := resolveHostname(true); len(ips) > 0 {
		var b strings.Builder
		b.WriteString(ips[0].String())
		for _, ip := range ips[1:] {
			b.WriteRune(';')
			b.WriteString(ip.String())
		}
		return toValue(b.String())
	}
	private := []string{"10.0.0.0", "172.16.0.0", "192.168.0.0", "FC00::"}
	ips := probeRoutes(private)
	return toValue(ips)
}

func probeRoutes(addresses []string) string {
	var slice []string
	set := map[string]struct{}{}
	for _, address := range addresses {
		localAddr := probeRoute(address)
		if localAddr == "" {
			continue
		}
		if _, ok := set[localAddr]; ok {
			continue
		}
		set[localAddr] = struct{}{}
		slice = append(slice, localAddr)
	}
	return strings.Join(slice, ";")
}

// probeRoute creates a UDP "connection" to the remote address, and returns the
// local interface address. This does involve a system call, but does not
// generate any network traffic since UDP is a connectionless protocol.
func probeRoute(address string) string {
	conn, err := net.Dial("udp", net.JoinHostPort(address, "80"))
	if err != nil {
		return ""
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	if local.IP.IsLoopback() ||
		local.IP.IsLinkLocalUnicast() ||
		local.IP.IsLinkLocalMulticast() {
		return ""
	}
	return local.IP.String()
}

// resolveHostname does a DNS resolve of the machine's hostname, and filters
// out any loopback and link-local addresses, as well as any IPv6 addresses if
// ipv6 is set to false.
func resolveHostname(ipv6 bool) []net.IP {
	host, err := os.Hostname()
	if err != nil {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	var addrs []net.IP
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			continue
		}
		if ip.To4() != nil || ipv6 {
			addrs = append(addrs, ip)
		}
	}
	return addrs
}

func dnsDomainLevels(call goja.FunctionCall) goja.Value {
	host := call.Argument(0).String()
	return toValue(strings.Count(host, "."))
}

func shExpMatch(call goja.FunctionCall) goja.Value {
	str := call.Argument(0).String()
	shexp := call.Argument(1).String()
	g, err := glob.Compile(shexp)
	if err != nil {
		return goja.Undefined()
	}
	return toValue(g.Match(str))
}

func weekdayRange(call goja.FunctionCall, now time.Time) goja.Value {
	if call.Argument(len(call.Arguments)-1).String() == "GMT" {
		now = now.In(time.UTC)
	}
	weekdays := map[string]time.Weekday{
		"SUN": time.Sunday, "MON": time.Monday, "TUE": time.Tuesday, "WED": time.Wednesday,
		"THU": time.Thursday, "FRI": time.Friday, "SAT": time.Saturday,
	}
	wd1, ok := weekdays[call.Argument(0).String()]
	if !ok {
		return goja.Undefined()
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

func dateRange(call goja.FunctionCall, now time.Time) goja.Value {
	argc := len(call.Arguments)
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
		arg := call.Argument(i)
		exported := arg.Export()
		switch v := exported.(type) {
		case float64:
			n := int64(v)
			if 1 <= n && n <= 31 {
				days = append(days, int(n))
			} else {
				years = append(years, int(n))
			}
		case int64:
			n := v
			if 1 <= n && n <= 31 {
				days = append(days, int(n))
			} else {
				years = append(years, int(n))
			}
		case int:
			n := int64(v)
			if 1 <= n && n <= 31 {
				days = append(days, int(n))
			} else {
				years = append(years, int(n))
			}
		default:
			if month, ok := monthmap[arg.String()]; ok {
				months = append(months, month)
			} else {
				return goja.Undefined()
			}
		}
	}

	switch max(len(days), len(months), len(years)) {
	case 1:
		if len(days) == 1 && days[0] != now.Day() {
			return toValue(false)
		} else if len(months) == 1 && months[0] != now.Month() {
			return toValue(false)
		} else if len(years) == 1 && years[0] != now.Year() {
			return toValue(false)
		} else {
			return toValue(true)
		}
	case 2:
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
		return goja.Undefined()
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

func timeRange(call goja.FunctionCall, now time.Time) goja.Value {
	argc := len(call.Arguments)
	if call.Argument(argc-1).String() == "GMT" {
		now = now.In(time.UTC)
		argc--
	}
	h1, m1, s1, h2, m2, s2 := 0, 0, 0, 0, 0, 0
	toInt := func(idx int) int {
		return int(call.Argument(idx).ToInteger())
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
		return goja.Undefined()
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), h1, m1, s1, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), h2, m2, s2, 0, now.Location())
	return toValue(!start.After(now) && end.After(now))
}
