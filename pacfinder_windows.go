// Copyright 2019, 2021, 2022, 2026 The Alpaca Authors
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
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type pacFinder struct{
	pacURL string
	auto   bool
}

func newPacFinder(pacURL string) *pacFinder {
	if pacURL != "" {
		return &pacFinder{pacURL: pacURL, auto: false}
	}

	_, pacURL, _, _, err := getPACURL()
	if err != nil {
		log.Print("Unable to access system network information")
		return &pacFinder{"", false}
	}
	return &pacFinder{pacURL: pacURL, auto: true}
}

func (finder *pacFinder) findPACURL() (string, error) {
	if !finder.auto {
		return finder.pacURL, nil
	}
	_, pacURL, _, _, err := getPACURL()
	return pacURL, err
}

func (finder *pacFinder) pacChanged() bool {
	if url, _ := finder.findPACURL(); finder.pacURL != url {
		finder.pacURL = url
		return true
	}
	return false
}

type BOOL int32

// WINHTTP_CURRENT_USER_IE_PROXY_CONFIG (winhttp.h)
type winhttpCurrentUserIEProxyConfig struct {
	// The Internet Explorer proxy configuration for the current user specifies "automatically detect settings".
	fAutoDetect BOOL
	// The auto-configuration URL if the Internet Explorer proxy configuration for the current user specifies "Use automatic proxy configuration".
	lpszAutoConfigUrl *uint16
	// The proxy URL if the Internet Explorer proxy configuration for the current user specifies "use a proxy server".
	lpszProxy *uint16
	// The optional proxy by-pass server list.
	lpszProxyBypass *uint16
}

var (
	modWinHttp     = windows.NewLazySystemDLL("winhttp.dll")
	procGetCfg     = modWinHttp.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	modKernel      = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalFree = modKernel.NewProc("GlobalFree")
)

func getPACURL() (autoDetect bool, autoConfigURL string, proxy string, proxyBypass string, err error) {
	var cfg winhttpCurrentUserIEProxyConfig

	// WINHTTPAPI BOOL WinHttpGetIEProxyConfigForCurrentUser(
	//  [in, out] WINHTTP_CURRENT_USER_IE_PROXY_CONFIG *pProxyConfig
	// );
	r1, _, e := procGetCfg.Call(uintptr(unsafe.Pointer(&cfg)))
	if r1 == 0 {
		if e != syscall.Errno(0) {
			err = e
		} else {
			err = syscall.EINVAL
		}
		return
	}
	defer func() {
		// if they are non-NULL. Use GlobalFree to free the strings.
		globalFreeForLPWSTR(cfg.lpszAutoConfigUrl)
		globalFreeForLPWSTR(cfg.lpszProxy)
		globalFreeForLPWSTR(cfg.lpszProxyBypass)
	}()

	autoDetect = cfg.fAutoDetect != 0
	if cfg.lpszAutoConfigUrl != nil {
		autoConfigURL = windows.UTF16PtrToString(cfg.lpszAutoConfigUrl)
	}
	if cfg.lpszProxy != nil {
		proxy = windows.UTF16PtrToString(cfg.lpszProxy)
	}
	if cfg.lpszProxyBypass != nil {
		proxyBypass = windows.UTF16PtrToString(cfg.lpszProxyBypass)
	}
	return
}

func globalFreeForLPWSTR(ptr *uint16) {
	if ptr != nil {
		procGlobalFree.Call(uintptr(unsafe.Pointer(ptr)))
	}
}
