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
	"net/url"
	"path/filepath"
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
	pacFinder *pacFinder
	monitor   netMonitor
	client    *http.Client
	connected bool
	//cache  []byte
	//modified time.Time
	//fetched time.Time
	//expiry   time.Time
	//etag     string
}

func newPACFetcher(pacurl string) *pacFetcher {
	client := &http.Client{Timeout: 30 * time.Second}
	client.Transport = &transportWrapper{}
	u, err := url.Parse(pacurl)

	if err == nil {
		if !u.IsAbs() && validateFileUri(u) == nil {
			// If URI already contains file: scheme it can't legally contain relative
			// file path. Therefore if relative file path is needed don't use the scheme
			p, err := filepath.Abs(u.Path)
			if err == nil {
				p = fileToUriPath(p)
				u = &url.URL{
					Scheme: "file",
					Path:   p,
				}
				pacurl = u.String()
			}
		}
		if strings.EqualFold(u.Scheme, "file") {
			log.Print("Warning: When using a local PAC file, the online/offline status can't ",
				"be determined by the fact that the PAC file is downloaded. Make sure you ",
				"check for proxy connectivity in your PAC file!")
		}
	}
	return &pacFetcher{
		pacFinder: newPacFinder(pacurl),
		monitor:   newNetMonitor(),
		client:    client,
	}
}

// On Windows adds the necessary forward slash before the drive letter or UNC path
// to make it a valid file URI. On other platforms, it just converts the path to
// forward slashes.
func fileToUriPath(p string) string {
	p = filepath.ToSlash(p)
	if (runtime.GOOS == "windows" && len(p) >= 2 && p[1] == ':') ||
		(strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///")) {
		// Add an extra leading slash for Windows drive letter or UNC path
		p = "/" + p
	}
	return p
}

type transportWrapper struct {
	http.RoundTripper
}

// Modifies the request URL when necessary and calls the delegate transport
func (t *transportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL
	scheme := u.Scheme

	var delegate http.RoundTripper
	if strings.EqualFold(scheme, "file") {
		fs, err := fixFileRequestUrl(req)
		if err != nil {
			return nil, err
		}

		delegate = http.NewFileTransport(fs)
	} else {
		// The DefaultClient in net/http uses the proxy specified in the http(s)_proxy
		// environment variable, which could be pointing at this instance of alpaca. When
		// fetching the PAC file, we always use a client that goes directly to the server,
		// rather than via a proxy.
		delegate = &http.Transport{Proxy: nil}
	}
	return delegate.RoundTrip(req)
}

// Modifies the request URL to be compatible with http.NewFileTransport
// and returns the file system root. It also validates the file URI.
func fixFileRequestUrl(req *http.Request) (http.FileSystem, error) {
	// file:a/b - opaque=a/b, path= , not supported
	// file:/a/b - path=/a - java canonical
	// => "/", "a/b"
	// file://a/b - host=a, not supported
	// file:///a/b - absolute file, path=/a/b, volume name ""
	// => "/", "a/b"
	// file:///C:/a/b - absolute file, path=/C:/a/b, volume name bad
	// => "C:\\", "a/b"
	// file:////a/b/c - path=//a/b/c, UNC, java canonical, volume name \\a\b
	// => "\\\\a\\b\\", "c"
	// file://///a/b/c - path=///a, UNC, browser canonical, volume name bad
	// => "\\\\a\\b\\", "c"
	u := req.URL
	p, err := extractNativePath(u)
	if err != nil {
		return nil, err
	}

	fsRoot := "/"
	if runtime.GOOS == "windows" {
		volName := filepath.VolumeName(p)
		if volName != "" {
			// internal path joiner omits the separator if fs root ends with a colon:
			// bad result: "C:folder\subfolder"
			fsRoot = volName + "\\"
			remainder := p[min(len(fsRoot), len(p)):]
			u = &url.URL{
				Scheme: "file",
				Path:   remainder,
			}
			req.URL = u
		}
	}
	return http.Dir(fsRoot), nil
}

// On Windows removes one leading slash in "/C:/..." or "///host/share/...".
// Forwards slashes are not converted to backslashes.
func extractNativePath(u *url.URL) (string, error) {
	err := validateFileUri(u)
	if err != nil {
		return "", err
	}

	p := u.Path
	if runtime.GOOS == "windows" {
		// the result of NewFileTransport doesn't support drive letters in URLs, extracting into fs root
		if strings.HasPrefix(p, "/") && (strings.HasPrefix(p, "///") || (len(p) >= 3 && p[2] == ':')) {
			// keep two slashes in UNC or no slashes in drive letter
			p = p[1:]
		}
	}
	return p, nil
}

// Validates possibly relative file uri
func validateFileUri(uri *url.URL) error {
	if uri.Opaque != "" {
		return fmt.Errorf("URI is not hierarchical")
	}
	if uri.User != nil || uri.Host != "" {
		return fmt.Errorf("URI has an authority component")
	}
	return nil
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
