package gateway

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Host and WireGuard monitoring consume the same short-lived kernel snapshot.
// Policies, rate deltas and retention remain owned by each monitor.
type interfaceCounterCache struct {
	mu   sync.Mutex
	at   time.Time
	rows []any
	err  error
}

func parseInterfaceCounters(raw string) ([]any, error) {
	rows := []any{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		rows = append(rows, M{"name": strings.TrimSpace(parts[0]), "receive_total": rx, "send_total": tx})
	}
	if len(rows) == 0 {
		return nil, errors.New("接口流量计数暂不可读取")
	}
	return rows, nil
}
func (c *interfaceCounterCache) snapshot(stamp time.Time, read func() ([]byte, error)) ([]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || stamp.Sub(c.at) >= time.Second {
		raw, err := read()
		c.rows, c.err = nil, err
		if err == nil {
			c.rows, c.err = parseInterfaceCounters(string(raw))
		}
		c.at = stamp
	}
	return clone(c.rows), c.err
}
func (a *App) interfaceCounters() ([]any, error) {
	return a.interfaceCache.snapshot(time.Now(), func() ([]byte, error) { return os.ReadFile("/proc/net/dev") })
}
