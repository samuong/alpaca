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
	"bytes"
	"log"
	"net/http"
	"text/template"
)

// PACData contains program configuration to be made available to the pacWrapTmpl.
type PACData struct {
	Port int
}

type pacData struct {
	PACData
	UpstreamPAC string
}

type PACWrapper struct {
	data      pacData
	tmpl      *template.Template
	alpacaPAC string
}

// PACWrapper template for serving a PAC file to point at alpaca or DIRECT. If we have a valid
// PAC file, we wrap that PAC file with a wrapper function that only returns "DIRECT" or
// "localhost:port". If we do not have a PAC file, the PAC function we serve only returns "DIRECT",
// which should prevent all requests reaching us.
var pacWrapTmpl = `// Wrapped for and by alpaca
function FindProxyForURL(url, host) {
{{ if .UpstreamPAC }}
  return FindProxyForURL(url, host) === "DIRECT" ? "DIRECT" : "PROXY localhost:{{.Port}}";
{{.UpstreamPAC}}
{{ else }}
  return "DIRECT";
{{ end }}
}
`

func NewPACWrapper(data PACData) *PACWrapper {
	t := template.Must(template.New("alpaca").Parse(pacWrapTmpl))
	return &PACWrapper{pacData{data, ""}, t, ""}
}

func (pw *PACWrapper) Wrap(pacjs []byte) {
	pac := string(pacjs)
	if pac == pw.data.UpstreamPAC && pw.alpacaPAC != "" {
		return
	}
	pw.data.UpstreamPAC = pac
	b := &bytes.Buffer{}
	if err := pw.tmpl.Execute(b, pw.data); err != nil {
		log.Printf("error executing PAC wrap template: %v", err)
		return
	}
	pw.alpacaPAC = b.String()
}

func (pw *PACWrapper) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/alpaca.pac", pw.handlePAC)
}

func (pw *PACWrapper) handlePAC(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	if _, err := w.Write([]byte(pw.alpacaPAC)); err != nil {
		log.Printf("Error writing PAC to response: %v", err)
	}
}
