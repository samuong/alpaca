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
	"context"
	"net/http"
	"sync/atomic"
)

type contextKey string

const contextKeyID = contextKey("id")

// AddContextID wraps a http.Handler to add a strictly increasing uint to the
// context of the http.Request with the key "id" as it passes through the
// request to the next handler.
func AddContextID(next http.Handler) http.Handler {
	var id uint64
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(
			req.Context(), contextKeyID, atomic.AddUint64(&id, 1),
		)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
