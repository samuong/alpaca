// Copyright 2019 The Alpaca Authors
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
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

func findPACURL() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return findPACURLForDarwin()
	case "windows":
		return findPACURLForWindows()
	default:
		// Hopefully Linux, FreeBSD, Solaris, etc. will have GNOME 3 installed...
		// TODO: Figure out how to do this for KDE.
		return findPACURLForGNOME()
	}
}

func findPACURLForDarwin() (string, error) {
	cmd := exec.Command("networksetup", "-listallnetworkservices")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer cmd.Wait()
	r := bufio.NewReader(stdout)
	// Discard the first line, which isn't the name of a network service.
	if _, err := r.ReadString('\n'); err != nil {
		return "", err
	}
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		// An asterisk (*) denotes that a network service is disabled; ignore it.
		networkService := strings.TrimSuffix(strings.TrimPrefix(line, "(*)"), "\n")
		url, err := getAutoProxyURL(networkService)
		if err != nil {
			log.Printf("Error getting auto proxy URL for %v: %v", networkService, err)
			continue
		} else if url == "(null)" {
			continue
		}
		return url, nil
	}
	return "", nil
}

func getAutoProxyURL(networkService string) (string, error) {
	cmd := exec.Command("networksetup", "-getautoproxyurl", networkService)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer cmd.Wait()
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		if !strings.HasPrefix(line, "URL: ") {
			// Ignore lines without a URL, including the "Enabled" line. Assume that any
			// disabled network services might come back online at some point, in which
			// case we should start using the PAC URL for that service.
			continue
		}
		return strings.TrimSuffix(strings.TrimPrefix(line, "URL: "), "\n"), nil
	}
	return "", fmt.Errorf("No auto-proxy URL for network service %v", networkService)
}

func findPACURLForWindows() (string, error) {
	// TODO: Implement this.
	return "", nil
}

func findPACURLForGNOME() (string, error) {
	cmd := exec.Command("gsettings", "get", "org.gnome.system.proxy", "autoconfig-url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.Trim(string(out), "'\n"), nil
}
