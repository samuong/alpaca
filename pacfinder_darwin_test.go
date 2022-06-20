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
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindPACURLStatic(t *testing.T) {
	pac := "http://internal.anz.com/proxy.pac"
	finder := newPacFinder(pac)

	foundPac, _ := finder.findPACURL()
	require.Equal(t, pac, foundPac)
}

func TestFindPACURL(t *testing.T) {
	finder := newPacFinder("")

	foundPac, _ := finder.findPACURL()

	require.NotEqual(t, "", foundPac)
}

// Removed TestFindPACURLWhenNetworkSetupIsntAvailable - we don't rely on NetworkSetup anymore

func TestFallbackToDefaultWhenNoPACUrl(t *testing.T) {
	// arrange
	cmdStr := "scutil --proxy | awk '/ProxyAutoConfigURLString/ {printf $3}'"
	cmd := exec.Command("bash", "-c", cmdStr)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	finder := newPacFinder("")

	// act
	foundPac, _ := finder.findPACURL()

	// assert
	require.Equal(t, out.String(), foundPac)
}
