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

//go:build aix || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"os/exec"
	"strings"
)

type pacFinder struct {
	pacUrl string
	auto   boolean
}

func newPacFinder(pacUrl string) *pacFinder {
	return &pacFinder{pacUrl, pacUrl == ""}
}

func (finder *pacFinder) findPACURL() (string, error) {
	if !finder.auto {
		return finder.pacUrl, nil
	}

	// Hopefully Linux, FreeBSD, Solaris, etc. will have GNOME 3 installed...
	// TODO: Figure out how to do this for KDE.
	cmd := exec.Command("gsettings", "get", "org.gnome.system.proxy", "autoconfig-url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.Trim(string(out), "'\n"), nil
}

func (finder *pacFinder) pacChanged() bool {
	if url, _ := finder.findPACURL(); finder.pacUrl != url {
		finder.pacUrl = url
		return true
	}

	return false
}
