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

static inline SCDynamicStoreRef SCDynamicStoreCreate_trampoline() {
	return SCDynamicStoreCreate(kCFAllocatorDefault, CFSTR("alpaca"), NULL, NULL);
}

static inline const char *CFStringToCString(CFStringRef value) {
	if (value == NULL) {
		return "";
	}

	const char *constValue = CFStringGetCStringPtr(value, kCFStringEncodingUTF8);
	if (constValue != NULL) {
		// Don't release value here as CFStringGetCStringPtr returns the raw c string backing the CFString
		return constValue;
	}

	CFIndex length = CFStringGetLength(value);
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1;
	if (length == 0 || maxSize == 0) {
		CFRelease(value);
		return "";
	}

	char *cValue = (char *)malloc(maxSize);
	if (CFStringGetCString(value, cValue, length, kCFStringEncodingUTF8)) {
		CFRelease(value);
		return (const char *)cValue;
	}

	CFRelease(value);
	free(cValue);
	return "";
}

static inline const char *GetPACUrl_trampoline(SCDynamicStoreRef store) {
	CFDictionaryRef dict = SCDynamicStoreCopyValue(store, CFSTR("State:/Network/Global/Proxies"));
	CFStringRef url = CFDictionaryGetValue(dict, CFSTR("ProxyAutoConfigURLString"));
	CFRetain(url);

	// Free the dict to avoid any leaks
	CFRelease(dict);

	return CFStringToCString(url);
}
*/
import "C"
import (
	"log"
	"time"
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

	var url string
	start := time.Now()

	if cUrl := C.GetPACUrl_trampoline(finder.storeRef); cUrl != nil {
		url = C.GoString(cUrl)
	}

	elapsed := time.Since(start)
	log.Printf("PacUrl found in %v", elapsed)

	return url, nil
}

func (finder *pacFinder) pacChanged() bool {
	if url, _ := finder.findPACURL(); finder.pacUrl != url {
		finder.pacUrl = url
		return true
	}

	return false
}
