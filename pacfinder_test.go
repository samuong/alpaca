package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFindPACURLForDarwin(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.Nil(t, os.Setenv("PATH", oldpath))
	require.Nil(t, os.Setenv("PATH", dir+":"+oldpath))

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
	require.Nil(t, ioutil.WriteFile(tmpfn, []byte(mockcmd), 0700))

	pacURL, err := findPACURLForDarwin()
	require.Nil(t, err)
	assert.Equal(t, "http://internal.anz.com/proxy.pac", pacURL)
}

func TestFindPACURLForDarwinWhenNetworkSetupIsntAvailable(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.Nil(t, os.Setenv("PATH", oldpath))
	require.Nil(t, os.Setenv("PATH", dir))
	_, err = findPACURLForDarwin()
	require.NotNil(t, err)
}

func TestFindPACURLForGNOME(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.Nil(t, os.Setenv("PATH", oldpath))

	require.Nil(t, os.Setenv("PATH", dir))
	tmpfn := filepath.Join(dir, "gsettings")
	mockcmd := "#!/bin/sh\necho \\'http://internal.anz.com/proxy.pac\\'\n"
	require.Nil(t, ioutil.WriteFile(tmpfn, []byte(mockcmd), 0700))

	pacURL, err := findPACURLForGNOME()
	require.Nil(t, err)
	assert.Equal(t, "http://internal.anz.com/proxy.pac", pacURL)
}

func TestFindPACURLForGNOMEWhenGsettingsIsntAvailable(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	oldpath := os.Getenv("PATH")
	defer require.Nil(t, os.Setenv("PATH", oldpath))
	require.Nil(t, os.Setenv("PATH", dir))
	_, err = findPACURLForGNOME()
	require.NotNil(t, err)
}
