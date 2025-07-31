// Copyright 2019, 2021, 2022, 2025 The Alpaca Authors
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
*/
import "C"
import (
	"log"
	"unsafe"
)

type pacFinder struct {
	pacUrl   string
	storeRef C.SCDynamicStoreRef
}

func newPacFinder(pacUrl string) *pacFinder {
	if pacUrl != "" {
		return &pacFinder{pacUrl, 0}
	}
	storeRef := C.SCDynamicStoreCreate_trampoline()
	if storeRef == 0 {
		log.Print("Unable to access system network information")
		return &pacFinder{"", 0}
	}
	return &pacFinder{"", storeRef}
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

	proxySettings := C.SCDynamicStoreCopyProxies(finder.storeRef)
	if proxySettings == 0 {
		// log.Printf("No proxy settings found using SCDynamicStoreCopyProxies")
		return "", nil
	}
	defer C.CFRelease(C.CFTypeRef(proxySettings))

	kSCPropNetProxiesProxyAutoConfigEnable := CFStringCreateWithCString("ProxyAutoConfigEnable")
	kSCPropNetProxiesProxyAutoConfigURLString := CFStringCreateWithCString("ProxyAutoConfigURLString")

	pacEnabled := C.CFNumberRef(C.CFDictionaryGetValue(proxySettings, unsafe.Pointer(kSCPropNetProxiesProxyAutoConfigEnable)))
	if pacEnabled == 0 {
		// log.Printf("PAC enable flag not found in proxy settings using SCDynamicStoreCopyProxies")
		return "", nil
	}

	var enabled C.int
	if C.CFNumberGetValue(pacEnabled, C.kCFNumberIntType, unsafe.Pointer(&enabled)) == 0 {
		// log.Printf("Could not retrieve value of PAC enabled flag using SCDynamicStoreCopyProxies")
		return "", nil
	}

	if enabled == 0 {
		// log.Printf("PAC is not enabled using SCDynamicStoreCopyProxies")
		return "", nil
	}

	pacURL := C.CFStringRef(C.CFDictionaryGetValue(proxySettings, unsafe.Pointer(kSCPropNetProxiesProxyAutoConfigURLString)))
	if pacURL == 0 {
		// log.Printf("PAC URL not found in proxy settings using SCDynamicStoreCopyProxies")
		return "", nil
	}

	return CFStringToString(pacURL), nil
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
