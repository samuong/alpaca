package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

type proxyHandler struct {
	pacRunner *PacRunner
}

func NewProxyHandler(pacUrl string) (http.Handler, error) {
	resp, err := http.DefaultClient.Get(pacUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return newProxyHandler(resp.Body)
}

func NewDirectProxyHandler() (http.Handler, error) {
	return newProxyHandler(strings.NewReader(
		`function FindProxyForURL(url, host) { return "DIRECT" }`))
}

func newProxyHandler(r io.Reader) (http.Handler, error) {
	pr, err := NewPacRunner(r)
	if err != nil {
		return nil, err
	}
	return proxyHandler{pr}, nil
}

func (ph proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		connect(w, r)
	} else {
		direct(w, r)
	}
}

func connect(w http.ResponseWriter, r *http.Request) {
	// TODO: should probably put a timeout on this
	server, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("Can't hijack connection to %v", r.Host)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, _, err := h.Hijack()
	if err != nil {
		// The response status has already been sent, so if hijacking
		// fails, we can't return an error status to the client.
		// Instead, log the error and finish up.
		log.Printf("Error hijacking connection to %v", r.Host)
		server.Close()
		return
	}
	go func() {
		defer server.Close()
		defer client.Close()
		var wg sync.WaitGroup
		wg.Add(2)
		go transfer(&wg, server, client)
		go transfer(&wg, client, server)
		wg.Wait()
	}()
}

func transfer(wg *sync.WaitGroup, dst, src net.Conn) {
	defer wg.Done()
	_, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Error copying from %v to %v",
			src.RemoteAddr().String(), dst.RemoteAddr().String())
	}
}

func direct(w http.ResponseWriter, r *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	// TODO: Don't retransmit hop-by-hop headers.
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers#hbh
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		// The response status has already been sent, so if copying
		// fails, we can't return an error status to the client.
		// Instead, log the error.
		log.Printf("Error copying response body from %v", r.Host)
		return
	}
}
