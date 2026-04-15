// Copyright 2019, 2020, 2021, 2023, 2024 The Alpaca Authors
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
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toJSArg(arg interface{}) string {
	switch v := arg.(type) {
	case string:
		b, _ := json.Marshal(v)
		return string(b)
	case int, int8, int16, int32, int64, float32, float64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func TestDirect(t *testing.T) {
	var pr PACRunner
	pacjs := []byte(`function FindProxyForURL(url, host) { return "DIRECT" }`)
	require.NoError(t, pr.Update(pacjs))
	proxy, err := pr.FindProxyForURL(url.URL{Scheme: "https", Host: "anz.com"})
	require.NoError(t, err)
	assert.Equal(t, "DIRECT", proxy)
}

func TestFindProxyForURL(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"NoScheme", "//alpaca.test", "https://alpaca.test/"},
		{"HTTP", "http://alpaca.test/a?b=c#d", "http://alpaca.test/a?b=c#d"},
		{"HTTPS", "https://alpaca.test/a?b=c#d", "https://alpaca.test/"},
		{"WSS", "wss://alpaca.test/a?b=c#d", "wss://alpaca.test/"},
	}
	for _, test := range tests {
		var pr PACRunner
		pacjs := []byte("function FindProxyForURL(url, host) { return url }")
		require.NoError(t, pr.Update(pacjs))
		t.Run(test.name, func(t *testing.T) {
			u, err := url.Parse(test.input)
			require.NoError(t, err)
			proxy, err := pr.FindProxyForURL(*u)
			require.NoError(t, err)
			assert.Equal(t, test.expected, proxy)
		})
	}
}

func TestIsPlainHostName(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"www", true},
		{"anz.com", false},
	}
	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("isPlainHostName", isPlainHostName))
			value, err := vm.RunString("isPlainHostName(" + fmt.Sprintf("%q", test.host) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestDnsDomainIs(t *testing.T) {
	tests := []struct {
		host, domain string
		expected     bool
	}{
		{"www.anz.com", ".anz.com", true},
		{"www", ".anz.com", false},
		{"notanz.com", ".anz.com", false},
		{"notanz.com", "anz.com", true}, // https://crbug.com/299649
	}
	for _, test := range tests {
		t.Run(test.host+" "+test.domain, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("dnsDomainIs", dnsDomainIs))
			value, err := vm.RunString("dnsDomainIs(" + fmt.Sprintf("%q", test.host) + ", " + fmt.Sprintf("%q", test.domain) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestLocalHostOrDomainIs(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		hostdom  string
		expected bool
	}{
		{"exact match", "www.mozilla.org", "www.mozilla.org", true},
		{"hostname match", "www", "www.mozilla.org", true},
		{"domain name mismatch", "www.google.com", "www.mozilla.org", false},
		{"hostname mismatch", "home.mozilla.org", "www.mozilla.org", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("localHostOrDomainIs", localHostOrDomainIs))
			value, err := vm.RunString("localHostOrDomainIs(" + fmt.Sprintf("%q", test.host) + ", " + fmt.Sprintf("%q", test.hostdom) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestIsResolvable(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"localhost", true},
		{"nonexistent.test", false},
	}
	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("isResolvable", isResolvable))
			value, err := vm.RunString("isResolvable(" + fmt.Sprintf("%q", test.host) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestIsInNet(t *testing.T) {
	tests := []struct {
		host     string
		pattern  string
		mask     string
		expected bool
	}{
		{"localhost", "127.0.0.0", "255.0.0.0", true},
		{"192.0.2.1", "192.0.2.0", "255.255.255.0", true},
		{"192.0.3.1", "192.0.2.0", "255.255.255.0", false},
		{"192.0.3.1", "192.0.2.0", "255.255.0.0", true},
		{"192.0.3.1", "192.0.2.0", "255.255.255", false},
	}
	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("isInNet", isInNet))
			value, err := vm.RunString("isInNet(" + fmt.Sprintf("%q", test.host) + ", " + fmt.Sprintf("%q", test.pattern) + ", " + fmt.Sprintf("%q", test.mask) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestDnsResolve(t *testing.T) {
	tests := []struct {
		host     string
		expected string
	}{
		{"localhost", "127.0.0.1"},
		{"192.0.2.1", "192.0.2.1"},
	}
	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("dnsResolve", dnsResolve))
			value, err := vm.RunString("dnsResolve(" + fmt.Sprintf("%q", test.host) + ")")
			require.NoError(t, err)
			actual := value.Export().(string)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestConvertAddr(t *testing.T) {
	tests := []struct {
		ipaddr   string
		expected int64
	}{
		{"104.16.41.2", 1745889538},
		{"2001:db8::", 0},
		{"www.anz.com", 0},
	}
	for _, test := range tests {
		t.Run(test.ipaddr, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("convert_addr", convertAddr))
			value, err := vm.RunString("convert_addr(" + fmt.Sprintf("%q", test.ipaddr) + ")")
			require.NoError(t, err)
			actual := value.ToInteger()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestMyIpAddress(t *testing.T) {
	vm := goja.New()
	require.NoError(t, vm.Set("myIpAddress", myIpAddress))
	value, err := vm.RunString("myIpAddress()")
	require.NoError(t, err)
	output := value.ToString()
	actualOutput := output.Export().(string)
	assert.NotNil(t, net.ParseIP(actualOutput))
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	for _, addr := range addrs {
		if strings.HasPrefix(addr.String(), actualOutput) {
			return
		}
	}
	t.Fail()
}

func TestDnsDomainLevels(t *testing.T) {
	tests := []struct {
		host     string
		expected int64
	}{
		{"www", 0},
		{"mozilla.org", 1},
		{"www.mozilla.org", 2},
	}
	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("dnsDomainLevels", dnsDomainLevels))
			value, err := vm.RunString("dnsDomainLevels(" + fmt.Sprintf("%q", test.host) + ")")
			require.NoError(t, err)
			actual := value.ToInteger()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestShExpMatch(t *testing.T) {
	tests := []struct {
		str, shexp string
		expected   bool
	}{
		{"http://anz.com/a/b/c.html", "*/b/*", true},
		{"http://anz.com/d/e/f.html", "*/b/*", false},
	}
	for _, test := range tests {
		t.Run(test.str+" "+test.shexp, func(t *testing.T) {
			vm := goja.New()
			require.NoError(t, vm.Set("shExpMatch", shExpMatch))
			value, err := vm.RunString("shExpMatch(" + fmt.Sprintf("%q", test.str) + ", " + fmt.Sprintf("%q", test.shexp) + ")")
			require.NoError(t, err)
			actual := value.ToBoolean()
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestWeekdayRange(t *testing.T) {
	tests := []struct {
		name         string
		args         []interface{}
		expectations string
	}{
		{"M-F AEST", []interface{}{"MON", "FRI"}, "NYYYYYN"},
		{"M-F UTC", []interface{}{"MON", "FRI", "GMT"}, "NNYYYYY"},
		{"SAT AEST", []interface{}{"SAT"}, "NNNNNNY"},
		{"SAT UTC", []interface{}{"SAT", "GMT"}, "YNNNNNN"},
		{"F&M ONLY", []interface{}{"FRI", "MON"}, "NYNNNYN"},
	}

	// The reference time is 10 hours ahead of UTC, so 5am is 7pm on the previous day in UTC.
	sunday, err := time.Parse(time.RFC1123Z, "Sun, 30 Jun 2019 05:00:00 +1000")
	require.NoError(t, err)
	weekdays := []struct {
		name string
		t    time.Time
	}{
		{"SUN", sunday},
		{"MON", sunday.Add(1 * 24 * time.Hour)},
		{"TUE", sunday.Add(2 * 24 * time.Hour)},
		{"WED", sunday.Add(3 * 24 * time.Hour)},
		{"THU", sunday.Add(4 * 24 * time.Hour)},
		{"FRI", sunday.Add(5 * 24 * time.Hour)},
		{"SAT", sunday.Add(6 * 24 * time.Hour)},
	}

	for _, test := range tests {
		for i, weekday := range weekdays {
			t.Run(test.name+" "+weekday.name, func(t *testing.T) {
				vm := goja.New()
				f := func(fc goja.FunctionCall) goja.Value {
					return weekdayRange(fc, weekday.t)
				}
				require.NoError(t, vm.Set("weekdayRange", f))
				args := make([]string, len(test.args))
				for j, arg := range test.args {
					args[j] = toJSArg(arg)
				}
				value, err := vm.RunString("weekdayRange(" + strings.Join(args, ", ") + ")")
				require.NoError(t, err)
				actual := value.ToBoolean()
				expected := test.expectations[i] == 'Y'
				assert.Equal(t, expected, actual)
			})
		}
	}
}

func TestDateRange(t *testing.T) {
	tests := []struct {
		name         string
		args         []interface{}
		expectations map[string]bool
	}{
		{
			"returns true on the first day of each month, local timezone",
			[]interface{}{1},
			map[string]bool{
				"2019-06-30": false,
				"2019-07-01": true,
				"2019-07-02": false,
				"2019-08-01": true,
			},
		}, {
			// All of these dates are tested with a time of 5am in AEST, which is 10
			// hours ahead of UTC. So 5am in AEST is 7pm on the previous day in UTC.
			"returns true on the first day of each month, GMT timezone",
			[]interface{}{1, "GMT"},
			map[string]bool{
				"2019-07-01": false,
				"2019-07-02": true,
				"2019-07-03": false,
				"2019-08-02": true,
			},
		}, {
			"returns true on the first half of each month",
			[]interface{}{1, 15},
			map[string]bool{
				"2019-06-30": false,
				"2019-07-01": true,
				"2019-07-02": true,
				"2019-07-15": true,
				"2019-07-16": false,
			},
		}, {
			"returns true on 24th of December each year",
			[]interface{}{24, "DEC"},
			map[string]bool{
				"2019-12-23": false,
				"2019-12-24": true,
				"2019-12-26": false,
				"2020-12-24": true,
			},
		}, {
			"returns true on the first quarter of the year",
			[]interface{}{"JAN", "MAR"},
			map[string]bool{
				"2018-12-31": false,
				"2019-01-01": true,
				"2019-01-02": true,
				"2019-03-31": true,
				"2019-04-01": false,
				"2020-03-31": true,
			},
		}, {
			"returns true from June 1st until August 15th, each year",
			[]interface{}{1, "JUN", 15, "AUG"},
			map[string]bool{
				"2019-05-31": false,
				"2019-06-01": true,
				"2019-06-02": true,
				"2019-08-15": true,
				"2019-08-16": false,
				"2020-08-15": true,
			},
		}, {
			"returns true from June 1st, 1995, until August 15th, same year",
			[]interface{}{1, "JUN", 1995, 15, "AUG", 1995},
			map[string]bool{
				"1995-05-31": false,
				"1995-06-01": true,
				"1995-06-02": true,
				"1995-08-15": true,
				"1995-08-16": false,
				"1996-08-15": false,
			},
		}, {
			"returns true from October 1995 until March 1996",
			[]interface{}{"OCT", 1995, "MAR", 1996},
			map[string]bool{
				"1995-09-30": false,
				"1995-10-01": true,
				"1995-10-02": true,
				"1996-01-01": true,
				"1996-03-31": true,
				"1996-04-01": false,
				"1997-01-01": false,
			},
		}, {
			"returns true during the entire year of 1995",
			[]interface{}{1995},
			map[string]bool{
				"1994-12-31": false,
				"1995-01-01": true,
				"1995-04-19": true,
				"1995-12-31": true,
				"1996-01-01": false,
			},
		}, {
			"returns true from beginning of year 1995 until the end of year 1997",
			[]interface{}{1995, 1997},
			map[string]bool{
				"1994-12-31": false,
				"1995-01-01": true,
				"1996-04-19": true,
				"1997-12-31": true,
				"1998-01-01": false,
			},
		},
	}

	check := func(t *testing.T, args []interface{}, date string, expected bool) {
		vm := goja.New()
		now, err := time.Parse(time.RFC3339, date+"T05:00:00+10:00")
		require.NoError(t, err)
		f := func(fc goja.FunctionCall) goja.Value { return dateRange(fc, now) }
		require.NoError(t, vm.Set("dateRange", f))
		argsStr := make([]string, len(args))
		for i, arg := range args {
			argsStr[i] = toJSArg(arg)
		}
		value, err := vm.RunString("dateRange(" + strings.Join(argsStr, ", ") + ")")
		require.NoError(t, err)
		actual := value.ToBoolean()
		assert.Equal(t, expected, actual)
	}

	for _, test := range tests {
		for date, expected := range test.expectations {
			t.Run(test.name+" "+date, func(t *testing.T) {
				check(t, test.args, date, expected)
			})
		}
	}
}

func TestTimeRange(t *testing.T) {
	tests := []struct {
		name         string
		args         []interface{}
		expectations map[string]bool
	}{
		{
			"returns true from noon to 1pm",
			[]interface{}{12},
			map[string]bool{
				"11:59:59": false,
				"12:00:00": true,
				"12:00:01": true,
				"12:59:59": true,
				"13:00:00": false,
			},
		}, {
			"returns true from noon to 1pm",
			[]interface{}{12, 13},
			map[string]bool{
				"11:59:59": false,
				"12:00:00": true,
				"12:00:01": true,
				"12:59:59": true,
				"13:00:00": false,
			},
		}, {
			// Local time (AEST) is 10 hours ahead of UTC, so we expect timeRange to
			// return true from 10pm to 11pm AEST.
			"true from noon to 1pm, in GMT timezone",
			[]interface{}{12, "GMT"},
			map[string]bool{
				"21:59:59": false,
				"22:00:00": true,
				"22:00:01": true,
				"22:59:59": true,
				"23:00:00": false,
			},
		}, {
			"returns true from 9am to 5pm",
			[]interface{}{9, 17},
			map[string]bool{
				"08:59:59": false,
				"09:00:00": true,
				"09:00:01": true,
				"16:59:59": true,
				"17:00:00": false,
			},
		}, {
			"returns true from 8:30am to 5:00pm",
			[]interface{}{8, 30, 17, 00},
			map[string]bool{
				"08:29:59": false,
				"08:30:00": true,
				"08:30:01": true,
				"16:59:59": true,
				"17:00:00": false,
			},
		}, {
			"returns true between midnight and 30 seconds past midnight",
			[]interface{}{0, 0, 0, 0, 0, 30},
			map[string]bool{
				"00:00:00": true,
				"00:00:01": true,
				"00:00:29": true,
				"00:00:30": false,
				"23:59:59": false,
			},
		},
	}

	check := func(t *testing.T, args []interface{}, mocktime string, expected bool) {
		vm := goja.New()
		now, err := time.Parse(time.RFC3339, "2019-07-01T"+mocktime+"+10:00")
		require.NoError(t, err)
		f := func(fc goja.FunctionCall) goja.Value { return timeRange(fc, now) }
		require.NoError(t, vm.Set("timeRange", f))
		argsStr := make([]string, len(args))
		for i, arg := range args {
			argsStr[i] = toJSArg(arg)
		}
		value, err := vm.RunString("timeRange(" + strings.Join(argsStr, ", ") + ")")
		require.NoError(t, err)
		actual := value.ToBoolean()
		assert.Equal(t, expected, actual)
	}

	for _, test := range tests {
		for mocktime, expected := range test.expectations {
			t.Run(test.name+" "+mocktime, func(t *testing.T) {
				check(t, test.args, mocktime, expected)
			})
		}
	}
}
