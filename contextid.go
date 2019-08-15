package main

import (
	"context"
	"net/http"
)

// AddContextID wraps a http.Handler to add a monotonically increasing
// uint to the context of the http.Request with the key "id" (string)
// as it passes through the request to the next handler.
func AddContextID(next http.Handler) http.Handler {
	ids := make(chan uint)
	go func() {
		for id := uint(0); ; id++ {
			ids <- id
		}
	}()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), "id", <-ids)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
