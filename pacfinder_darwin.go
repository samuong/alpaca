// Copyright 2019, 2021 The Alpaca Authors
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

	return &pacFinder{"", C.SCDynamicStoreCreate_trampoline()}
}

func (finder *pacFinder) findPACURL() (string, error) {
	if finder.storeRef == 0 {
		return finder.pacUrl, nil
	}

	//start := time.Now()
	url := finder.getPACUrl()

	//elapsed := time.Since(start)
	//log.Printf("PacUrl found in %v", elapsed)

	return url, nil
}

func (finder *pacFinder) pacChanged() bool {
	if url, _ := finder.findPACURL(); finder.pacUrl != url {
		finder.pacUrl = url
		return true
	}

	return false
}

func (finder *pacFinder) getPACUrl() string {
	dict := C.CFDictionaryRef(C.SCDynamicStoreCopyValue(finder.storeRef, C.kProxiesSettings))

	if dict == 0 {
		return ""
	}

	defer C.CFRelease(C.CFTypeRef(dict))

	pacEnabled := C.CFNumberRef(C.CFDictionaryGetValue(dict, unsafe.Pointer(C.kProxiesProxyAutoConfigEnable)))
	if pacEnabled == 0 {
		return ""
	}

	var enabled C.int
	C.CFNumberGetValue(pacEnabled, C.kCFNumberIntType, unsafe.Pointer(&enabled))
	if enabled == 0 {
		return ""
	}

	url := C.CFStringRef_Const(C.CFDictionaryGetValue(dict, unsafe.Pointer(C.kProxiesAutoConfigURLString)))

	if url == 0 {
		return ""
	}

	return CFStringToString(url)
}

// CGO Helpers below..

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
	_ = C.CFStringGetBytes(s, C.CFRange{0, length}, C.kCFStringEncodingUTF8, C.UInt8(0), C.false, (*C.UInt8)(&buf[0]), maxBufLen, &usedBufLen)
	return string(buf[:usedBufLen])
}
