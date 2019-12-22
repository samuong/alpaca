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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"testing"

	"github.com/keybase/go-keychain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeExecCommand(env []string, name string, arg ...string) *exec.Cmd {
	arg = append([]string{"-test.run=TestMockDefaults", "--", name}, arg...)
	cmd := exec.Command(os.Args[0], arg...)
	cmd.Env = append(env, "ALPACA_WANT_MOCK_DEFAULTS=1")
	return cmd
}

func TestMockDefaults(t *testing.T) {
	if os.Getenv("ALPACA_WANT_MOCK_DEFAULTS") != "1" {
		return
	}
	args := os.Args
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == len(os.Args) {
		fmt.Println("no command")
		os.Exit(2)
	} else if cmd := args[0]; cmd != "defaults" {
		fmt.Printf("%s: command not found\n", cmd)
		os.Exit(127)
	} else if len(args) != 4 || args[1] != "read" {
		fmt.Println("usage: defaults read <domain> <key>")
		os.Exit(255)
	}
	domain, key := args[2], args[3]
	if os.Getenv("DOMAIN_EXISTS") != "1" {
		fmt.Printf("Domain %s does not exist\n", domain)
		os.Exit(1)
	} else if key == "UseKeychain" && os.Getenv("USE_KEYCHAIN") == "1" {
		fmt.Println(1)
		os.Exit(0)
	} else if key == "UserPrincipal" {
		switch os.Getenv("USER_PRINCIPAL") {
		case "1":
			fmt.Println("malory")
			os.Exit(0)
		case "2":
			fmt.Println("malory@ISIS")
			os.Exit(0)
		}
	}
	fmt.Printf("The domain/default pair of (%s, %s) does not exist\n", domain, key)
	os.Exit(1)
}

func TestNoMADNotConfigured(t *testing.T) {
	env := []string{"DOMAIN_EXISTS=0"}
	execCommand = func(n string, a ...string) *exec.Cmd { return fakeExecCommand(env, n, a...) }
	_, err := getCredentialsFromNoMAD()
	require.Error(t, err)
}

func TestNoMADNotUsingKeychain(t *testing.T) {
	env := []string{"DOMAIN_EXISTS=1", "USE_KEYCHAIN=0"}
	execCommand = func(n string, a ...string) *exec.Cmd { return fakeExecCommand(env, n, a...) }
	_, err := getCredentialsFromNoMAD()
	require.Error(t, err)
}

func TestNoMADNoUserPrincipal(t *testing.T) {
	env := []string{"DOMAIN_EXISTS=1", "USE_KEYCHAIN=1", "USER_PRINCIPAL=0"}
	execCommand = func(n string, a ...string) *exec.Cmd { return fakeExecCommand(env, n, a...) }
	_, err := getCredentialsFromNoMAD()
	require.Error(t, err)
}

func TestNoMADInvalidUserPrincipal(t *testing.T) {
	env := []string{"DOMAIN_EXISTS=1", "USE_KEYCHAIN=1", "USER_PRINCIPAL=1"}
	execCommand = func(n string, a ...string) *exec.Cmd { return fakeExecCommand(env, n, a...) }
	_, err := getCredentialsFromNoMAD()
	require.Error(t, err)
}

func TestNoMAD(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	kc, err := keychain.NewKeychain(path.Join(dir, "test.keychain"), "")
	require.NoError(t, err)
	testKeychain = &kc

	p := keychain.NewGenericPassword("", "malory@ISIS", "NoMAD", []byte("guest"), "")
	p.SetAccessible(keychain.AccessibleWhenUnlocked)
	p.UseKeychain(kc)
	require.NoError(t, keychain.AddItem(p))

	env := []string{"DOMAIN_EXISTS=1", "USE_KEYCHAIN=1", "USER_PRINCIPAL=2"}
	execCommand = func(n string, a ...string) *exec.Cmd { return fakeExecCommand(env, n, a...) }
	auth, err := getCredentialsFromNoMAD()
	require.NoError(t, err)
	assert.Equal(t, "ISIS", auth.domain)
	assert.Equal(t, "malory", auth.username)
	assert.Equal(t, "guest", auth.password)
}
