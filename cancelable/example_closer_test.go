// Copyright 2020 The Alpaca Authors
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

package cancelable_test

import (
	"fmt"

	"github.com/samuong/alpaca/cancelable"
)

type printCloser struct {
	msg string
}

// Close prints the printCloser's msg on stdout.
func (pc *printCloser) Close() error {
	fmt.Printf("Close() called: %s\n", pc.msg)
	return nil
}

func ExampleCloser() {
	c := create(false) // defer in create() prints once
	c.Close()          // prints again

	c = create(true) // defer cancelled, no print
	c.Close()        // prints

	// Output:
	// Close() called: doCancel: false
	// Close() called: doCancel: false
	// Close() called: doCancel: true
}

func create(doCancel bool) *printCloser {
	pc := &printCloser{fmt.Sprintf("doCancel: %v", doCancel)}

	closer := cancelable.NewCloser(pc)
	defer closer.Close()

	if !doCancel {
		return pc // deferred Close will cause msg to be printed
	}

	closer.Cancel() // Cancel will prevent Close being called from defer
	return pc
}
