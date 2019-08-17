package main

import (
	"bytes"
	"log"
	"net/http"
	"text/template"
)

type PACData struct {
	Port     int
	PACURL   string
	Domain   string
	Username string
}

type pacData struct {
	PACData
	PAC string
}

type PACWrapper struct {
	data      pacData
	tmpl      *template.Template
	alpacaPAC string
}

// PACWrapper template for serving a PAC file to point at alpaca or DIRECT.
// If we have a valid PAC file, we wrap that PAC file with a wrapper
// function that only returns "DIRECT" or "localhost:port". If we do not
// have a PAC file, the PAC function we serve only returns "DIRECT", which
// should prevent all requests reaching us.
var pacWrapTmpl = `// Wrapped for and by alpaca
function FindProxyForURL(url, host) {
{{ if .PAC }}
  return FindProxyForURL(url, host) === "DIRECT" ? "DIRECT" : "PROXY localhost:{{.Port}}";
{{.PAC}}
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
	if pac == pw.data.PAC && pw.alpacaPAC != "" {
		return
	}
	pw.data.PAC = pac
	b := &bytes.Buffer{}
	if err := pw.tmpl.Execute(b, pw.data); err != nil {
		log.Printf("error executing PAC wrap template: %v\n", err)
		return
	}
	pw.alpacaPAC = b.String()
}

func (pw *PACWrapper) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/proxy.pac", pw.handleProxyPAC)
	mux.HandleFunc("/alpaca.pac", pw.handleAlpacaPAC)
}

func (pw *PACWrapper) handleProxyPAC(w http.ResponseWriter, req *http.Request) {
	pw.handlePAC(w, req, pw.data.PAC)
}

func (pw *PACWrapper) handleAlpacaPAC(w http.ResponseWriter, req *http.Request) {
	pw.handlePAC(w, req, pw.alpacaPAC)
}

func (pw *PACWrapper) handlePAC(w http.ResponseWriter, req *http.Request, pac string) {
	if req.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	if _, err := w.Write([]byte(pac)); err != nil {
		log.Printf("Error writing PAC to response: %v\n", err)
	}
}
