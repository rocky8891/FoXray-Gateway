package gateway

import "testing"

func TestHomeHeaderAutoHidePreference(t *testing.T) {
	a, f := fixture(t)
	if a.presentationPolicy()["home_header_auto_hide"] != false {
		t.Fatal("header must default visible")
	}
	if num(a.presentationPolicy()["home_header_idle_seconds"]) != 5 {
		t.Fatal("idle duration must default to 5 seconds")
	}
	if err := a.savePresentation(M{"home_header_idle_seconds": 75}); err != nil || num(a.presentationPolicy()["home_header_idle_seconds"]) != 75 {
		t.Fatal("idle duration was not saved")
	}
	beforeCalls := len(f.calls)
	for _, on := range []bool{true, false} {
		if err := a.savePresentation(M{"home_header_auto_hide": on}); err != nil {
			t.Fatal(err)
		}
		if a.presentationPolicy()["home_header_auto_hide"] != on {
			t.Fatal("header preference not persisted")
		}
	}
	before := fingerprint(a.settings)
	for _, invalid := range []any{0, 0.5, 3601, "30"} {
		if a.savePresentation(M{"home_header_idle_seconds": invalid}) == nil {
			t.Fatal("invalid idle duration accepted")
		}
	}
	if a.savePresentation(M{"home_header_auto_hide": "true"}) == nil {
		t.Fatal("string accepted as boolean")
	}
	if fingerprint(a.settings) != before || len(f.calls) != beforeCalls {
		t.Fatal("display preference changed runtime or invalid input changed settings")
	}
}
