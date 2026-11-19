package gateway

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInterfaceSnapshotSharedConcurrentAndExpires(t *testing.T) {
	var cache interfaceCounterCache
	stamp := time.Now()
	reads := 0
	read := func() ([]byte, error) { reads++; return []byte("wg-home: 123 0 0 0 0 0 0 0 456 0 0 0 0 0 0 0\n"), nil }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := cache.snapshot(stamp, read)
			if err != nil || len(rows) != 1 {
				t.Error("missing shared snapshot")
			}
			obj(rows[0])["send_total"] = 999
		}()
	}
	wg.Wait()
	if reads != 1 {
		t.Fatalf("read %d times", reads)
	}
	rows, err := cache.snapshot(stamp.Add(500*time.Millisecond), read)
	if err != nil || num(obj(rows[0])["send_total"]) != 456 {
		t.Fatal("consumer mutated shared cache")
	}
	_, _ = cache.snapshot(stamp.Add(2*time.Second), read)
	if reads != 2 {
		t.Fatal("snapshot did not expire")
	}
	rows, err = cache.snapshot(stamp.Add(4*time.Second), func() ([]byte, error) { return nil, errors.New("unavailable") })
	if err == nil || len(rows) != 0 {
		t.Fatal("failed collection reused old counters")
	}
}
func TestInterfaceCounterParsingSkipsInvalidAndPreservesZero(t *testing.T) {
	rows, err := parseInterfaceCounters("invalid: x 0 0 0 0 0 0 0 12 0 0 0 0 0 0 0\nwg0: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	if err != nil || len(rows) != 1 || str(obj(rows[0])["name"]) != "wg0" {
		t.Fatal(rows, err)
	}
	if _, err = parseInterfaceCounters("garbage"); err == nil {
		t.Fatal("invalid data marked available")
	}
}
func TestWireGuardConsumesSharedInterfaceSnapshot(t *testing.T) {
	a, _ := fixture(t)
	a.components["wireguard"] = true
	a.traffic.wgNames = []string{"wg-home", "wg-missing"}
	a.traffic.wgNamesAt = time.Now()
	_, err := a.interfaceCache.snapshot(time.Now(), func() ([]byte, error) { return []byte("wg-home: 123 0 0 0 0 0 0 0 456 0 0 0 0 0 0 0\n"), nil })
	if err != nil {
		t.Fatal(err)
	}
	counters, meta := map[string]trafficCounter{}, map[string]M{}
	a.traffic.wireguardCounters(counters, meta)
	row := counters["wg:wg-home"]
	if !row.Available || row.Up != 456 || row.Down != 123 || str(meta["wg:wg-home"]["source"]) != "WireGuard" {
		t.Fatal("WireGuard did not reuse host interface snapshot")
	}
	if counters["wg:wg-missing"].Available {
		t.Fatal("missing interface marked available")
	}
}
