// Copyright 2019, 2021, 2022, 2025 The Alpaca Authors
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
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/sandrolain/httpcache"
)

const maxResponseBytes = 1 * 1024 * 1024
const maxDataURLLength = 512 * 1024 * 1024

var delayAfterFailedDownload = 2 * time.Second

type pacFetcher struct {
	pacFinder *pacFinder
	monitor   netMonitor
	client    *http.Client
	connected bool
}

func newPACFetcher(pacurl string) *pacFetcher {
	var client *http.Client

	if strings.HasPrefix(pacurl, "file:") {
		log.Print("Warning: When using a local PAC file, the online/offline status can't ",
			"be determined by the fact that the PAC file is downloaded. Make sure you ",
			"check for proxy connectivity in your PAC file!")

		client = &http.Client{Timeout: 30 * time.Second}

		if runtime.GOOS == "windows" {
			client.Transport = http.NewFileTransport(http.Dir("C:"))
		} else {
			client.Transport = http.NewFileTransport(http.Dir("/"))
		}

	} else {
		// Base transport without proxy (important: avoid proxy loop)
		baseTransport := &http.Transport{Proxy: nil}

		// ✅ Use in-memory cache (FIXED)
		cacheTransport := httpcache.NewTransport(httpcache.NewMemoryCache())
		cacheTransport.Transport = baseTransport

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: cacheTransport,
		}
	}

	return &pacFetcher{
		pacFinder: newPacFinder(pacurl),
		monitor:   newNetMonitor(),
		client:    client,
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

func decodeDataURL(uri string) ([]byte, error) {
	parsedURL, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("error parsing pac url: %w", err)
	}

	if parsedURL.Scheme != "data" {
		return nil, nil
	}

	if len(uri) > maxDataURLLength {
		return nil, fmt.Errorf("error parsing data URL: PAC JS is too big (limit is %d bytes)",
			maxDataURLLength)
	}

	metadata, data, ok := strings.Cut(parsedURL.Opaque, ",")
	if !ok {
		return nil, fmt.Errorf("error parsing data URL: invalid format")
	}

	if strings.HasSuffix(metadata, ";base64") {
		bytes, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("error decoding base64 data URL: %w", err)
		}
		return bytes, nil
	}

	decoded, err := url.PathUnescape(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing data URL: %w", err)
	}
	return []byte(decoded), nil
}

func (pf *pacFetcher) download() []byte {
	if !pf.monitor.addrsChanged() && !pf.pacFinder.pacChanged() {
		return nil
	}
	pf.connected = false

	pf.client.CloseIdleConnections()

	pacurl, err := pf.pacFinder.findPACURL()
	if err != nil {
		log.Printf("Error while trying to detect PAC URL: %v", err)
		return nil
	} else if pacurl == "" {
		log.Println("No PAC URL specified or detected; all requests will be made directly")
		return nil
	}

	log.Printf("Attempting to download PAC from %s", pacurl)

	pac, err := decodeDataURL(pacurl)
	if err != nil {
		log.Printf("Error downloading PAC file: %v", err)
		return nil
	}

	if pac != nil {
		pf.connected = true
		return pac
	}

	resp, err := requireOK(pf.client.Get(pacurl))
	if err != nil {
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