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
	"fmt"
	"io"
	"log"
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
	pacFinder  *pacFinder
	monitor    netMonitor
	client     *http.Client
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
		log.Print("Warning: When using a local PAC file, the online/offline status can't ",
			"be determined by the fact that the PAC file is downloaded. Make sure you ",
			"check for proxy connectivity in your PAC file!")
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
		pacFinder:  newPacFinder(pacurl),
		monitor:    newNetMonitor(),
		client:     client,
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
	if !pf.monitor.addrsChanged() && !pf.pacFinder.pacChanged() {
		return nil
	}
	pf.connected = false

	pacurl, err := pf.pacFinder.findPACURL()
	if err != nil {
		log.Printf("Error while trying to detect PAC URL: %v", err)
		return nil
	} else if pacurl == "" {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
		return nil
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
		pf.connected = true
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
