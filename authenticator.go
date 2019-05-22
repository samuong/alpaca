package main

import (
	"bufio"
	"encoding/base64"
	"github.com/Azure/go-ntlmssp"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

type authenticator struct {
	domain, username, password string
}

func (a authenticator) connect(req *http.Request, conn net.Conn, rd *bufio.Reader) (*http.Response, error) {
	hostname, _ := os.Hostname() // in case of error, just use the zero value ("") as hostname
	negotiate, err := ntlmssp.NewNegotiateMessage(a.domain, hostname)
	if err != nil {
		log.Printf("Error creating NTLM Type 1 (Negotiate) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization", "NTLM "+base64.StdEncoding.EncodeToString(negotiate))
	if err := req.Write(conn); err != nil {
		log.Printf("Error sending NTLM Type 1 (Negotiate) request: %v", err)
		return nil, err
	}
	resp, err := http.ReadResponse(rd, req)
	if err != nil {
		log.Printf("Error receiving NTLM Type 2 (Challenge) response: %v", err)
		return nil, err
	} else if resp.StatusCode != http.StatusProxyAuthRequired {
		log.Printf("Expected response with status 407, got %s", resp.Status)
		return resp, nil
	}
	defer resp.Body.Close()
	challenge, err := base64.StdEncoding.DecodeString(
		strings.TrimPrefix(resp.Header.Get("Proxy-Authenticate"), "NTLM "))
	if err != nil {
		log.Printf("Error decoding NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	authenticate, err := ntlmssp.ProcessChallenge(challenge, a.username, a.password)
	if err != nil {
		log.Printf("Error processing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization",
		"NTLM "+base64.StdEncoding.EncodeToString(authenticate))
	if err := req.Write(conn); err != nil {
		log.Printf("Error sending NTLM Type 3 (Authenticate) request: %v", err)
		return nil, err
	}
	return http.ReadResponse(rd, req)
}

func (a authenticator) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	hostname, _ := os.Hostname() // in case of error, just use the zero value ("") as hostname
	negotiate, err := ntlmssp.NewNegotiateMessage(a.domain, hostname)
	if err != nil {
		log.Printf("Error creating NTLM Type 1 (Negotiate) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization", "NTLM "+base64.StdEncoding.EncodeToString(negotiate))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		log.Printf("Error sending NTLM Type 1 (Negotiate) request: %v", err)
		return nil, err
	} else if resp.StatusCode != http.StatusProxyAuthRequired {
		log.Printf("Expected response with status 407, got %s", resp.Status)
		return resp, nil
	}
	challenge, err := base64.StdEncoding.DecodeString(
		strings.TrimPrefix(resp.Header.Get("Proxy-Authenticate"), "NTLM "))
	if err != nil {
		log.Printf("Error decoding NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	authenticate, err := ntlmssp.ProcessChallenge(challenge, a.username, a.password)
	if err != nil {
		log.Printf("Error processing NTLM Type 2 (Challenge) message: %v", err)
		return nil, err
	}
	req.Header.Set("Proxy-Authorization",
		"NTLM "+base64.StdEncoding.EncodeToString(authenticate))
	return rt.RoundTrip(req)
}
