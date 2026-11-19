package gateway

import "testing"

func TestOverviewFooterDisplayPolicy(t *testing.T) {
	a, _ := fixture(t)
	for _, key := range []string{"overview_show_clock", "overview_show_data_status"} {
		if a.presentationPolicy()[key] != false {
			t.Fatal("footer must default off", key)
		}
		if err := a.savePresentation(M{key: true}); err != nil {
			t.Fatal(err)
		}
		if a.presentationPolicy()[key] != true {
			t.Fatal("display preference not saved", key)
		}
		if a.savePresentation(M{key: "true"}) == nil {
			t.Fatal("non-boolean display preference accepted", key)
		}
		if err := a.savePresentation(M{key: false}); err != nil {
			t.Fatal(err)
		}
	}
}
