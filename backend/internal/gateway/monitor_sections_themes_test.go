package gateway

import "testing"

func TestMonitorSectionsScopedPersistenceAndValidation(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	for _, module := range []string{"xray", "warp", "wireguard"} {
		for view, options := range monitorSectionOptions {
			key := "monitor_" + module + "_" + view + "_sections"
			if fingerprint(a.presentationPolicy()[key]) != fingerprint(options) {
				t.Fatal("legacy defaults missing", key)
			}
		}
	}
	if err := a.savePresentation(M{"monitor_xray_traffic_sections": []any{"statistics", "trend"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"monitor_warp_latency_sections": []any{}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"monitor_wireguard_connections_sections": []any{"details"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"content_width_percent": 90}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(arr(p["monitor_xray_traffic_sections"])[0]) != "statistics" || len(arr(p["monitor_warp_latency_sections"])) != 0 || len(arr(p["monitor_wireguard_connections_sections"])) != 1 {
		t.Fatal("scoped save lost sections")
	}
	if len(runner.calls) != calls {
		t.Fatal("layout save ran service commands")
	}
	for _, bad := range []M{
		{"monitor_xray_traffic_sections": []any{"trend", "trend"}},
		{"monitor_warp_latency_sections": []any{"statistics"}},
		{"monitor_wireguard_connections_sections": []any{"details", 1}},
		{"monitor_xray_traffic_sections": nil},
		{"monitor_xray_traffic_sections": "trend"},
		{"monitor_invalid_traffic_sections": []any{}},
	} {
		before := fingerprint(a.settings)
		if a.savePresentation(bad) == nil || fingerprint(a.settings) != before {
			t.Fatal("invalid layout changed settings")
		}
	}
	var stored M
	if err := readJSON(a.base+"/manager-settings.json", &stored); err != nil {
		t.Fatal(err)
	}
	if fingerprint(obj(stored["presentation"])["monitor_xray_traffic_sections"]) != fingerprint(p["monitor_xray_traffic_sections"]) {
		t.Fatal("layout not persisted")
	}
}

func TestSavedServiceThemesValidation(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	theme := M{"id": "custom-qa", "name": "自定义主题", "values": M{"services_host_style": "radial-wave", "services_xray_style": "radial-diamond-compact", "services_cards": []any{"service-host", "service-xray"}, "overview_text": M{"label_size": 16}}}
	if err := a.savePresentation(M{"service_custom_themes": []any{theme}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"monitor_xray_latency_sections": []any{"recent"}}); err != nil {
		t.Fatal(err)
	}
	if len(arr(a.presentationPolicy()["service_custom_themes"])) != 1 || len(runner.calls) != calls {
		t.Fatal("theme lost or service mutated")
	}
	for _, bad := range []any{
		nil, "invalid", []any{theme, theme},
		[]any{M{"id": "custom-invalid", "name": "bad", "values": M{"service_custom_themes": []any{theme}}}},
		[]any{M{"id": "custom-invalid", "name": "bad", "values": M{"content_width_percent": 90}}},
		[]any{M{"id": "custom-invalid", "name": "bad", "values": M{"services_host_style": "invalid"}}},
		[]any{M{"id": "custom-invalid", "name": "bad", "values": M{"overview_text": M{"value_size": 99}}}},
	} {
		before := fingerprint(a.settings)
		if a.savePresentation(M{"service_custom_themes": bad}) == nil || fingerprint(a.settings) != before {
			t.Fatal("invalid saved theme changed settings")
		}
	}
}
