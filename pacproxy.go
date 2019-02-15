package main

import (
	"io"
	"log"
	"net/http"
)

func handler(w http.ResponseWriter, r *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	http.HandleFunc("/", handler)
	// bind to localhost so that we only listen locally
	log.Fatal(http.ListenAndServe("localhost:3128", nil))
}
