// Copyright 2021 The Alpaca Authors
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerminal(t *testing.T) {
	fakeTerm := &terminal{
		readPassword: func() ([]byte, error) { return []byte("guest"), nil },
		stdout:       new(bytes.Buffer),
	}
	creds, err := fakeTerm.forUser("isis", "malory").getCredentials(true, false, "", "")
	require.NoError(t, err)
	require.NotNil(t, creds.ntlm)
	assert.Equal(t, "823893adfad2cda6e1a414f3ebdf58f7", creds.ntlm.hash)
}

func TestEnvVar(t *testing.T) {
	envVar := "malory@isis:823893adfad2cda6e1a414f3ebdf58f7"
	creds, err := fromEnvVar(envVar).getCredentials(true, false, "", "")
	require.NoError(t, err)
	require.NotNil(t, creds.ntlm)
	assert.Equal(t, "isis", creds.ntlm.domain)
	assert.Equal(t, "malory", creds.ntlm.username)
	assert.Equal(t, "823893adfad2cda6e1a414f3ebdf58f7", creds.ntlm.hash)
}

func TestEnvVarInvalid(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "NoAtDomain", input: "malory:823893adfad2cda6e1a414f3ebdf58f7"},
		{name: "NoColonHash", input: "malory@isis"},
		{name: "WrongOrder", input: "823893adfad2cda6e1a414f3ebdf58f7:malory@isis"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := fromEnvVar(test.input).getCredentials(true, false, "", "")
			assert.Error(t, err)
		})
	}
}
