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
	a, err := fakeTerm.forUser("isis", "malory").getCredentials()
	require.NoError(t, err)
	assert.Equal(t, "823893adfad2cda6e1a414f3ebdf58f7", a.hash)
}

func TestEnvVar(t *testing.T) {
	a, err := fromEnvVar("malory@isis:823893adfad2cda6e1a414f3ebdf58f7").getCredentials()
	require.NoError(t, err)
	assert.Equal(t, "isis", a.domain)
	assert.Equal(t, "malory", a.username)
	assert.Equal(t, "823893adfad2cda6e1a414f3ebdf58f7", a.hash)
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
			_, err := fromEnvVar(test.input).getCredentials()
			assert.Error(t, err)
		})
	}
}
