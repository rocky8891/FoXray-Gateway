package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

func (a *App) entryGeo(host string) M {
	a.geoMu.RLock()
	defer a.geoMu.RUnlock()
	return clone(obj(a.geo[host]))
}
func (a *App) geoAutomatic() {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			a.mu.RLock()
			nodes := clone(a.native)
			enabled := yes(a.components["xray"])
			a.mu.RUnlock()
			if enabled {
				for _, v := range arr(nodes["nodes"]) {
					if a.ctx.Err() != nil {
						return
					}
					n := obj(v)
					host := str(obj(n["outbound"])["server"])
					if host == "" {
						continue
					}
					old := a.entryGeo(host)
					ttl := 86400.
					if old["country"] == nil {
						ttl = 600
					}
					if now()-num(old["checked_at"]) < ttl {
						continue
					}
					row := M{"checked_at": now(), "status": "unknown"}
					ip, e := a.resolveEntry(host)
					address := net.ParseIP(ip)
					if e == nil && address != nil && address.IsGlobalUnicast() && !address.IsPrivate() {
						row["ip"] = ip
						ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
						req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.country.is/"+ip, nil)
						response, err := a.httpClient().Do(req)
						if err == nil {
							raw, _ := limitedRead(response.Body, 8192)
							response.Body.Close()
							data := M{}
							if response.StatusCode == 200 && json.Unmarshal(raw, &data) == nil {
								code := strings.ToUpper(str(data["country"]))
								if countryCode.MatchString(code) && code != "XX" && code != "ZZ" {
									row["country"], row["status"] = code, "located"
								}
							}
						}
						cancel()
					}
					a.geoMu.Lock()
					a.geo[host] = row
					copy := clone(a.geo)
					a.geoMu.Unlock()
					_ = writeJSON(filepath.Join(a.base, "geo-cache.json"), copy)
				}
			}
			timer.Reset(time.Minute)
		}
	}
}
