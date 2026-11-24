package gateway

import "testing"

func TestNativeRuleInventoryKeepsSystemRulesReadOnly(t *testing.T) {
	raw := "*filter\n:FORWARD ACCEPT [0:0]\n-A FORWARD -j OWNED\n-A OWNED -j ACCEPT\n-A FORWARD -s 192.0.2.0/24 -d 198.51.100.0/24 -m comment --comment \"test -j DROP\" -j ACCEPT\n-A FORWARD -i wg-test -j DROP\nCOMMIT\n"
	rows := identifyNativeRows(iptablesInventory(raw, "ipv4", map[string]bool{"OWNED": true}))
	if len(rows) != 2 || str(rows[0]["source"]) != "192.0.2.0/24" || str(rows[0]["destination"]) != "198.51.100.0/24" || str(rows[0]["action"]) != "accept" || str(rows[1]["action"]) != "drop" {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if yes(row["editable"]) || yes(row["managed"]) || !yes(row["runtime"]) || str(row["id"]) == "" {
			t.Fatal(row)
		}
	}
	counters := map[string]M{}
	rows, err := nftablesInventory(`{"nftables":[{"rule":{"family":"inet","table":"external","chain":"filter","handle":7,"expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":{"prefix":{"addr":"192.0.2.0","len":24}}}},{"counter":{"packets":3,"bytes":99}},{"accept":null}]}},{"rule":{"family":"inet","table":"owned","chain":"input","expr":[{"counter":{"packets":5,"bytes":88}},{"drop":null}]}}]}`, map[string]bool{"owned": true}, counters)
	if err != nil || len(rows) != 1 || str(rows[0]["source"]) != "192.0.2.0/24" || str(rows[0]["action"]) != "accept" || num(rows[0]["packets"]) != 3 || yes(rows[0]["editable"]) || num(counters["owned"]["packets"]) != 5 {
		t.Fatal(rows, counters, err)
	}
}
