package gateway

import "testing"

func TestThemeColorsAndClock(t *testing.T) {
	a, _ := fixture(t)
	if e := a.savePresentation(M{"overview_color_schemes": M{"dark": M{"services_wireguard_ring_color": "#123456"}, "light": M{"services_wireguard_ring_color": "#abcdef"}}, "clock_source": "server", "clock_timezone": "Asia/Shanghai"}); e != nil {
		t.Fatal(e)
	}
	for _, v := range []M{{"clock_timezone": "invalid"}, {"clock_source": "other"}, {"overview_color_schemes": M{"other": M{}}}} {
		if a.savePresentation(v) == nil {
			t.Fatal("bad settings accepted")
		}
	}
}
