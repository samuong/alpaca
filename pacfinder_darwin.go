// Copyright 2019, 2021, 2022 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

/*
#cgo LDFLAGS: -framework CoreFoundation -framework SystemConfiguration
#include <CoreFoundation/CoreFoundation.h>
#include <SystemConfiguration/SystemConfiguration.h>

static SCDynamicStoreRef SCDynamicStoreCreate_trampoline() {
    return SCDynamicStoreCreate(kCFAllocatorDefault, CFSTR("alpaca"), NULL, NULL);
}

typedef const CFStringRef CFStringRef_Const;
const CFStringRef_Const kProxiesSettings = CFSTR("State:/Network/Global/Proxies");
const CFStringRef_Const kProxiesAutoConfigURLString = CFSTR("ProxyAutoConfigURLString");
const CFStringRef_Const kProxiesProxyAutoConfigEnable = CFSTR("ProxyAutoConfigEnable");
*/
import "C"
import (
	"log"
	"unsafe"
)

type pacFinder struct {
	pacUrl   string
	storeRef C.SCDynamicStoreRef
	auto     bool
}

func newPacFinder(pacUrl string) *pacFinder {
	if pacUrl != "" {
		return &pacFinder{pacUrl, 0, false}
	}
	storeRef := C.SCDynamicStoreCreate_trampoline()
	if storeRef == 0 {
		log.Fatalf("Failed to create SCDynamicStore")
	}
	return &pacFinder{"", storeRef, true}
}

func (finder *pacFinder) pacChanged() bool {
	if url, _ := finder.findPACURL(); finder.pacUrl != url {
		finder.pacUrl = url
		return true
	}
	return false
}

func (finder *pacFinder) findPACURL() (string, error) {
	if finder.storeRef == 0 {
		return finder.pacUrl, nil
	}

	// First method: Using SCDynamicStoreCopyValue with specific key
	pacUrl := finder.getPACUrlFromSCDynamicStoreCopyValue()
	if pacUrl != "" {
		// log.Printf("Using PAC URL from SCDynamicStoreCopyValue method: %s", pacUrl)
		return pacUrl, nil
	}

	// Second method: Using SCDynamicStoreCopyProxies
	pacUrl = finder.getPACUrlFromSCDynamicStoreCopyProxies()
	if pacUrl != "" {
		// log.Printf("Using PAC URL from SCDynamicStoreCopyProxies method: %s", pacUrl)
		return pacUrl, nil
	}

	// log.Printf("No PAC URL found using either method")
	return "", nil
}

func (finder *pacFinder) getPACUrlFromSCDynamicStoreCopyValue() string {
	dict := C.CFDictionaryRef(C.SCDynamicStoreCopyValue(finder.storeRef, C.kProxiesSettings))
	if dict == 0 {
		// log.Printf("No proxy settings found in the dynamic store using SCDynamicStoreCopyValue")
		return ""
	}
	defer C.CFRelease(C.CFTypeRef(dict))

	pacEnabled := C.CFNumberRef(C.CFDictionaryGetValue(dict, unsafe.Pointer(C.kProxiesProxyAutoConfigEnable)))
	if pacEnabled == 0 {
		// log.Printf("PAC enable flag not found in proxy settings using SCDynamicStoreCopyValue")
		return ""
	}

	var enabled C.int
	if C.CFNumberGetValue(pacEnabled, C.kCFNumberIntType, unsafe.Pointer(&enabled)) == 0 {
		// log.Printf("Could not retrieve value of PAC enabled flag using SCDynamicStoreCopyValue")
		return ""
	}

	if enabled == 0 {
		// log.Printf("PAC is not enabled using SCDynamicStoreCopyValue")
		return ""
	}

	url := C.CFStringRef(C.CFDictionaryGetValue(dict, unsafe.Pointer(C.kProxiesAutoConfigURLString)))
	if url == 0 {
		// log.Printf("PAC URL string not found in proxy settings using SCDynamicStoreCopyValue")
		return ""
	}

	return CFStringToString(url)
}

func (finder *pacFinder) getPACUrlFromSCDynamicStoreCopyProxies() string {
	proxySettings := C.SCDynamicStoreCopyProxies(finder.storeRef)
	if proxySettings == 0 {
		// log.Printf("No proxy settings found using SCDynamicStoreCopyProxies")
		return ""
	}
	defer C.CFRelease(C.CFTypeRef(proxySettings))

	kSCPropNetProxiesProxyAutoConfigEnable := CFStringCreateWithCString("ProxyAutoConfigEnable")
	kSCPropNetProxiesProxyAutoConfigURLString := CFStringCreateWithCString("ProxyAutoConfigURLString")

	pacEnabled := C.CFNumberRef(C.CFDictionaryGetValue(proxySettings, unsafe.Pointer(kSCPropNetProxiesProxyAutoConfigEnable)))
	if pacEnabled == 0 {
		// log.Printf("PAC enable flag not found in proxy settings using SCDynamicStoreCopyProxies")
		return ""
	}

	var enabled C.int
	if C.CFNumberGetValue(pacEnabled, C.kCFNumberIntType, unsafe.Pointer(&enabled)) == 0 {
		// log.Printf("Could not retrieve value of PAC enabled flag using SCDynamicStoreCopyProxies")
		return ""
	}

	if enabled == 0 {
		// log.Printf("PAC is not enabled using SCDynamicStoreCopyProxies")
		return ""
	}

	pacURL := C.CFStringRef(C.CFDictionaryGetValue(proxySettings, unsafe.Pointer(kSCPropNetProxiesProxyAutoConfigURLString)))
	if pacURL == 0 {
		// log.Printf("PAC URL not found in proxy settings using SCDynamicStoreCopyProxies")
		return ""
	}

	return CFStringToString(pacURL)
}

// CFStringToString converts a CFStringRef to a string.
func CFStringToString(s C.CFStringRef) string {
	p := C.CFStringGetCStringPtr(s, C.kCFStringEncodingUTF8)
	if p != nil {
		return C.GoString(p)
	}

	length := C.CFStringGetLength(s)
	if length == 0 {
		return ""
	}

	maxBufLen := C.CFStringGetMaximumSizeForEncoding(length, C.kCFStringEncodingUTF8)
	if maxBufLen == 0 {
		return ""
	}

	buf := make([]byte, maxBufLen)
	var usedBufLen C.CFIndex
	_ = C.CFStringGetBytes(s, C.CFRange{0, length}, C.kCFStringEncodingUTF8, 0, C.Boolean(0), (*C.UInt8)(&buf[0]), maxBufLen, &usedBufLen)
	return string(buf[:usedBufLen])
}

// Helper function to create a CFStringRef from a Go string
func CFStringCreateWithCString(s string) C.CFStringRef {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))

	return C.CFStringCreateWithCString(C.kCFAllocatorDefault, cs, C.kCFStringEncodingUTF8)
}
