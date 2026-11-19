package gateway

import (
	"encoding/json"
	"io"
	"os"
)

// Read one small engine section without decoding potentially huge routing lists.
func readEngineField(path, key string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1024*1024))
	if _, err = d.Token(); err != nil {
		return err
	}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return e
		}
		if token == key {
			return d.Decode(value)
		}
		var skip json.RawMessage
		if e = d.Decode(&skip); e != nil {
			return e
		}
	}
	return io.EOF
}
func (a *App) probeConfigLocked() M {
	p := M{}
	for _, key := range []string{"endpoints", "interval", "latency_enabled", "latency_concurrency", "latency_url", "timeout", "speed_enabled", "speed_concurrency", "speed_timeout", "speed_size_mb", "speed_url"} {
		p[key] = clone(a.cfg[key])
	}
	return p
}
