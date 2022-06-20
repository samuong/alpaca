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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPACURL(t *testing.T) {
	dir, err := os.MkdirTemp("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.NoError(t, os.Setenv("PATH", oldpath))
	require.NoError(t, os.Setenv("PATH", dir+":"+oldpath))

	tmpfn := filepath.Join(dir, "networksetup")
	mockcmd := `#!/bin/sh
listallnetworkservices() {
	cat <<EOF
An asterisk (*) denotes that a network service is disabled.
iPhone USB
iPhone USB 2
(*)Wi-Fi
Bluetooth PAN
Thunderbolt Bridge
EOF
}

getautoproxyurl() {
	if [ "$1" = 'Wi-Fi' ]
	then
		cat <<EOF
URL: http://internal.anz.com/proxy.pac
Enabled: No
EOF
	elif [ "$1" = 'iPhone USB 2' ]
	then
		cat <<EOF
URL: 
Enabled: No
EOF
	else
		cat <<EOF
URL: (null)
Enabled: No
EOF
	fi
}

if [ "$1" = '-listallnetworkservices' ]
then
	listallnetworkservices "$2"
elif [ "$1" = '-getautoproxyurl' ]
then
	getautoproxyurl "$2"
else
	exit 1
fi

exit 0`
	require.NoError(t, os.WriteFile(tmpfn, []byte(mockcmd), 0700))

	pacURL, err := findPACURL()
	require.NoError(t, err)
	assert.Equal(t, "http://internal.anz.com/proxy.pac", pacURL)
}

func TestFindPACURLWhenNetworkSetupIsntAvailable(t *testing.T) {
	dir, err := os.MkdirTemp("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.NoError(t, os.Setenv("PATH", oldpath))
	require.NoError(t, os.Setenv("PATH", dir))
	_, err = findPACURL()
	require.NotNil(t, err)
}
