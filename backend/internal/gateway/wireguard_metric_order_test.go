package gateway

import (
	"path/filepath"
	"testing"
)

func TestWireGuardMetricOrdersPersistIndependently(t *testing.T) {
	a, f := fixture(t)
	calls := len(f.calls)
	orders := M{"wg-home": []any{"download", "upload", "latency", "connections"}, "wg-work": []any{"connections", "latency", "download", "upload"}}
	if err := a.savePresentation(M{"services_wireguard_metric_orders": orders}); err != nil {
		t.Fatal(err)
	}
	var saved M
	if err := readJSON(filepath.Join(a.base, "manager-settings.json"), &saved); err != nil {
		t.Fatal(err)
	}
	if fingerprint(obj(saved["presentation"])["services_wireguard_metric_orders"]) != fingerprint(orders) {
		t.Fatal("interface orders were not persisted")
	}
	if len(f.calls) != calls {
		t.Fatal("layout update ran network commands")
	}
	for _, invalid := range []any{nil, []any{}, M{"bad interface": orders["wg-home"]}, M{"wg-home": []any{"upload", "download", "latency", "latency"}}, M{"wg-home": []any{"upload", "download", "latency"}}, M{"wg-home": []any{"upload", "download", "latency", "unknown"}}} {
		before := fingerprint(a.settings)
		if err := a.savePresentation(M{"services_wireguard_metric_orders": invalid}); err == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid layout accepted or changed settings", invalid)
		}
	}
	if err := a.savePresentation(M{"service_custom_themes": []any{M{"id": "custom-wg", "name": "WG order", "values": M{"services_wireguard_metric_orders": orders}}}}); err != nil {
		t.Fatal("custom theme did not retain interface orders", err)
	}
}
