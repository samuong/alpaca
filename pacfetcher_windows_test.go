// Copyright 2025 The Alpaca Authors
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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnWindowsWithSubst(t *testing.T) {
	tempDir := t.TempDir()
	content := []byte(`function FindProxyForURL(url, host) { return "DIRECT" }`)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "proxy.pac"), content, 0644))

	cmd := exec.Command("subst", "T:", tempDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
	defer func() {
		cmd := exec.Command("subst", "T:", "/D")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		assert.NoError(t, cmd.Run())
	}()

	buf, err := os.ReadFile("T:\\proxy.pac")
	require.NoError(t, err)
	assert.Equal(t, string(content), string(buf))

	_, err = os.ReadFile("T:\\nonexistent.txt")
	require.Error(t, err)
}
