package gateway

import "testing"

func TestSubscriptionCreationTimePersistsAndLegacyMissingRemainsUnknown(t *testing.T) {
	a, _ := fixture(t)
	result, e := a.subscriptionAction(M{"name": "new", "url": "https://example.invalid/feed", "interval": 3600})
	if e != nil {
		t.Fatal(e)
	}
	id := str(result["id"])
	original := num(a.publicNative()["subscriptions"].([]M)[0]["created"])
	if original <= 0 {
		t.Fatal("creation time missing")
	}
	if _, e = a.subscriptionAction(M{"id": id, "name": "renamed", "created": 1}); e != nil {
		t.Fatal(e)
	}
	if num(a.publicNative()["subscriptions"].([]M)[0]["created"]) != original {
		t.Fatal("edit changed creation time")
	}
	a.native["subscriptions"] = []any{M{"id": "legacy", "name": "legacy", "interval": 3600}}
	if a.publicNative()["subscriptions"].([]M)[0]["created"] != nil {
		t.Fatal("legacy creation date invented")
	}
}
