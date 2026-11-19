package gateway

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// History contains numeric monitoring data only. Compress large responses while
// leaving small payloads and clients that disable gzip untouched.
func writeHistoryResponse(w http.ResponseWriter, r *http.Request, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		writeResponse(w, 500, M{"error": "历史数据编码失败"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept-Encoding")
	enabled := false
	for _, value := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		parts := strings.Split(strings.TrimSpace(value), ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		quality := 1.0
		for _, param := range parts[1:] {
			pair := strings.SplitN(strings.TrimSpace(param), "=", 2)
			if len(pair) == 2 && strings.EqualFold(pair[0], "q") {
				v, e := strconv.ParseFloat(pair[1], 64)
				if e != nil || v < 0 || v > 1 {
					quality = 0
				} else {
					quality = v
				}
			}
		}
		enabled = quality > 0
	}
	if enabled && len(raw) >= 4096 {
		out, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			writeResponse(w, 500, M{"error": "历史数据编码失败"})
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = out.Write(raw)
		_ = out.Close()
		return
	}
	_, _ = w.Write(raw)
}
