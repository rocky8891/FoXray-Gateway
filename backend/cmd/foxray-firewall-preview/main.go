// An isolated read-only firewall planner. It never starts component or monitoring jobs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/rocky8891/FoXray-Gateway/backend/internal/gateway"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

func main() {
	base := flag.String("base", "", "isolated preview data directory")
	listen := flag.String("listen", "127.0.0.1:18884", "loopback HTTP address")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || *base == "" {
		log.Fatal("isolated base and loopback listener required")
	}
	app, err := gateway.New(*base, *base, true)
	if err != nil {
		log.Fatal("preview initialization failed")
	}
	defer app.Close()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == "GET" && (r.URL.Path == "/api/network/firewall" || r.URL.Path == "/api/network/firewall/service")
		if r.Method == "POST" && r.URL.Path == "/api/network/plan" {
			raw, err := io.ReadAll(io.LimitReader(r.Body, 1048577))
			input := gateway.M{}
			allowed = err == nil && len(raw) <= 1048576 && json.Unmarshal(raw, &input) == nil && input["kind"] == "firewall"
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		if !allowed {
			http.Error(w, "read-only firewall preview", http.StatusConflict)
			return
		}
		app.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	log.Fatal(server.ListenAndServe())
}
