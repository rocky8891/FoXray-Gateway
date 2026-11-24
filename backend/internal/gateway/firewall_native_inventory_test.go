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

func TestIPTablesPriorityUsesPositionAmongManagedJumps(t *testing.T) {
	a, runner := fixture(t)
	firstRule := M{"id": "rule-one", "driver": "iptables", "priority": -20}
	secondRule := M{"id": "rule-two", "driver": "iptables", "priority": 10}
	a.network.managed["firewall"] = []any{firstRule, secondRule}
	runner.responses = map[string]string{
		"iptables -w 3 -t filter -S INPUT": "-P INPUT ACCEPT\n-A INPUT -j " + nativeChainName(firstRule) + "\n-A INPUT -j EXTERNAL\n-A INPUT -j " + nativeChainName(secondRule),
	}
	move := clone(firstRule)
	move["priority"] = 20
	position, err := a.network.nativeIPJumpIndex(move, false, true)
	if err != nil || position != 3 {
		t.Fatalf("expected insertion after the lower numeric priority and intervening external jump, got %d: %v", position, err)
	}
	position, err = a.network.nativeIPJumpIndex(firstRule, true, false)
	if err != nil || position != 1 {
		t.Fatalf("rollback must restore original position: %d, %v", position, err)
	}
}
