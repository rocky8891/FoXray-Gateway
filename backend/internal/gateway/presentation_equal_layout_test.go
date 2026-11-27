package gateway

import "testing"

func TestEqualOverviewLayoutsRequireTwoCards(t *testing.T) {
	for _, scope := range []struct {
		layout string
		cards  string
	}{
		{"services_layout", "services_cards"},
		{"overview_layout", "overview_cards"},
	} {
		for _, layout := range []string{"equal-rows", "equal-columns"} {
			t.Run(scope.layout+"/"+layout, func(t *testing.T) {
				a, _ := fixture(t)
				cards := arr(defaultPresentation()[scope.cards])
				for _, count := range []int{0, 1, 3, 4} {
					if err := a.savePresentation(M{scope.layout: layout, scope.cards: cards[:count]}); err == nil {
						t.Fatalf("accepted %s with %d cards", layout, count)
					}
				}
				if err := a.savePresentation(M{scope.layout: layout, scope.cards: cards[:2], "overview_show_clock": true}); err != nil {
					t.Fatal(err)
				}
				if got := a.presentationPolicy()[scope.layout]; got != layout {
					t.Fatalf("saved layout = %v, want %s", got, layout)
				}
				if err := a.savePresentation(M{scope.layout: "grid", scope.cards: cards[:3]}); err != nil {
					t.Fatalf("could not return to grid with three cards: %v", err)
				}
			})
		}
	}
}
