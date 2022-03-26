// Copyright 2019, 2021, 2022 The Alpaca Authors
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
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// The maximum size (in bytes) allowed for a PAC script. At 1 MB, this matches the limit in Chrome.
const maxResponseBytes = 1 * 1024 * 1024

// The time to wait before retrying a failed PAC download. This is similar to Chrome's delay:
// https://cs.chromium.org/chromium/src/net/proxy_resolution/proxy_resolution_service.cc?l=96&rcl=3db5f65968c3ecab3932c1ff7367ad28834f9502
var delayAfterFailedDownload = 2 * time.Second

type pacFetcher struct {
	pacurl     string
	monitor    netMonitor
	client     *http.Client
	lookupAddr func(context.Context, string) ([]string, error)
	connected  bool
	//cache  []byte
	//modified time.Time
	//fetched time.Time
	//expiry   time.Time
	//etag     string
}

func newPACFetcher(pacurl string) *pacFetcher {
	client := &http.Client{Timeout: 30 * time.Second}
	if strings.HasPrefix(pacurl, "file:") {
		log.Printf("Warning: Alpaca supports file:// PAC URLs, but Windows and macOS don't")
		if runtime.GOOS == "windows" {
			client.Transport = http.NewFileTransport(http.Dir("C:"))
		} else {
			client.Transport = http.NewFileTransport(http.Dir("/"))
		}
	} else {
		// The DefaultClient in net/http uses the proxy specified in the http(s)_proxy
		// environment variable, which could be pointing at this instance of alpaca. When
		// fetching the PAC file, we always use a client that goes directly to the server,
		// rather than via a proxy.
		client.Transport = &http.Transport{Proxy: nil}
	}
	return &pacFetcher{
		pacurl:     pacurl,
		monitor:    newNetMonitor(),
		client:     client,
		lookupAddr: net.DefaultResolver.LookupAddr,
	}
}

func requireOK(resp *http.Response, err error) (*http.Response, error) {
	if err != nil {
		return resp, err
	} else if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("expected status 200 OK, got %s", resp.Status)
	} else {
		return resp, nil
	}
}

func (pf *pacFetcher) download() []byte {
	if !pf.monitor.addrsChanged() {
		return nil
	}
	pf.connected = false
	pacurl := pf.pacurl
	if pacurl == "" {
		var err error
		pacurl, err = findPACURL()
		if err != nil {
			log.Printf("Error while trying to detect PAC URL: %v", err)
			return nil
		}
	}
	log.Printf("Attempting to download PAC from %s", pacurl)
	resp, err := requireOK(pf.client.Get(pacurl))
	if err != nil {
		// Sometimes, if we try to download too soon after a network change, the PAC
		// download can fail. See https://github.com/samuong/alpaca/issues/8 for details.
		log.Printf("Error downloading PAC file, will retry after %v: %q",
			delayAfterFailedDownload, err)
		time.Sleep(delayAfterFailedDownload)
		if resp, err = requireOK(pf.client.Get(pacurl)); err != nil {
			log.Printf("Error downloading PAC file, giving up: %q", err)
			return nil
		}
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err = io.CopyN(&buf, resp.Body, maxResponseBytes)
	if err == io.EOF {
		if strings.HasPrefix(pf.pacurl, "file:") {
			// When using a local PAC file the online/offline status can't be determined
			// by the fact that the PAC file is returned. Instead try reverse DNS
			// resolution of Google's Public DNS Servers.
			const timeout = 2 * time.Second
			ctx, cancel := context.WithTimeout(context.TODO(), timeout)
			defer cancel()
			_, err1 := pf.lookupAddr(ctx, "8.8.8.8")
			_, err2 := pf.lookupAddr(ctx, "2001:4860:4860::8888")
			if err1 == nil || err2 == nil {
				log.Printf("Successfully resolved public address; bypassing proxy")
			} else {
				pf.connected = true
			}
		} else {
			pf.connected = true
		}
		return buf.Bytes()
	} else if err != nil {
		log.Printf("Error reading PAC JS from response body: %q", err)
		return nil
	} else {
		log.Printf("PAC JS is too big (limit is %d bytes)", maxResponseBytes)
		return nil
	}
}

func (pf *pacFetcher) isConnected() bool {
	return pf.connected
}
