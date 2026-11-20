package gateway

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDNSPolicyCustomRulesPrecedeOptionalForceAndDirectList(t *testing.T) {
	for _, force := range []bool{false, true} {
		a, _ := fixture(t)
		c := clone(a.cfg)
		c["route_profile"] = "global"
		c["dns_force_proxy"] = force
		c["dns_direct_servers"] = []any{"114.114.114.114"}
		c["endpoints"] = []any{M{"id": "n1", "kind": "socks", "port": 1080, "enabled": true}}
		c["custom_rules"] = []any{M{"id": "r1", "enabled": true, "type": "ip", "network": "tcp,udp", "targets": []any{"114.114.114.114/32"}, "port": "53", "action": "block"}}
		engine, err := a.buildEngine(c, M{}, "n1")
		if err != nil {
			t.Fatal(err)
		}
		engine = clone(engine)
		tun := []M{}
		for _, r := range arr(obj(engine["routing"])["rules"]) {
			v := obj(r)
			if len(arr(v["inboundTag"])) > 0 && str(arr(v["inboundTag"])[0]) == "tun" {
				tun = append(tun, v)
			}
		}
		if len(tun) < 3 || tun[0]["outboundTag"] != "out-block" {
			t.Fatal("custom DNS rule not first", tun)
		}
		dns := tun[1]
		if dns["port"] != "53" {
			t.Fatal("DNS rule missing", dns)
		}
		if force {
			if dns["outboundTag"] != "out-n1" || dns["ip"] != nil {
				t.Fatal("force switch did not override direct list", dns)
			}
		} else {
			if dns["outboundTag"] != "out-direct" || len(arr(dns["ip"])) != 1 {
				t.Fatal("explicit DNS not direct", dns)
			}
			for _, r := range tun {
				if r["port"] == "53" && r["ip"] == nil && r["outboundTag"] == "out-n1" {
					t.Fatal("unconditional DNS force remains")
				}
			}
		}
		tags := map[string]bool{}
		for _, v := range arr(engine["outbounds"]) {
			tag := str(obj(v)["tag"])
			if tags[tag] {
				t.Fatal("duplicate outbound", tag)
			}
			tags[tag] = true
		}
	}
}
func TestObservationPolicyValidationAndSniffing(t *testing.T) {
	for _, bad := range []M{{"dns_direct_servers": []any{"example.com"}}, {"dns_direct_servers": []any{"1.1.1.1:53"}}, {"dns_direct_servers": []any{"0.0.0.0"}}, {"dns_force_proxy": "true"}, {"domain_identification": 1}} {
		if validateObservationPolicy(bad) == nil {
			t.Fatal("invalid accepted", bad)
		}
	}
	values, err := directDNSServers(M{"dns_direct_servers": []any{"1.1.1.1", "1.1.1.1", "2001:4860:4860::8888"}})
	if err != nil || len(values) != 2 {
		t.Fatal(values, err)
	}
	a, _ := fixture(t)
	c := clone(a.cfg)
	c["route_profile"] = "global"
	c["domain_identification"] = true
	c["endpoints"] = []any{M{"id": "n1", "kind": "socks", "port": 1080, "enabled": true}}
	engine, err := a.buildEngine(c, M{}, "n1")
	if err != nil {
		t.Fatal(err)
	}
	engine = clone(engine)
	sniff := obj(obj(arr(engine["inbounds"])[0])["sniffing"])
	if !yes(sniff["enabled"]) || !yes(sniff["routeOnly"]) || sniff["metadataOnly"] != false || len(arr(sniff["destOverride"])) != 3 {
		t.Fatal(sniff)
	}
	if obj(engine["log"])["access"] != "none" {
		t.Fatal("domain toggle enabled file logging")
	}
}
func TestWebhookDomainsUseExactTupleAndRespectSwitch(t *testing.T) {
	o := newRouteObserver()
	event := routeEvent{Timestamp: time.Now().Unix(), Inbound: "tun", Outbound: "out-n1", Source: "192.0.2.2:41000", Original: "tcp:198.51.100.3:443", Target: "tcp:WWW.Example.com:443", Network: "tcp", Protocol: "tls"}
	if !o.record(event) || len(o.domainHints()) != 0 {
		t.Fatal("collection should default off")
	}
	o.setDomainCollection(true)
	for _, protocol := range []string{"http", "tls", "quic"} {
		event.Protocol = protocol
		event.Source = net.JoinHostPort("192.0.2.2", strconv.Itoa(41000+len(protocol)))
		if !o.record(event) {
			t.Fatal("event rejected")
		}
		key := eventKey(event)
		evidence := o.domainHints()[key]
		if evidence.domain != "www.example.com" || !strings.Contains(evidence.note, "Webhook") {
			t.Fatal(evidence)
		}
	}
	rows := []M{{"module": "xray", "protocol": "tcp", "source": event.Source, "destination": "198.51.100.3:443"}, {"module": "xray", "protocol": "tcp", "source": event.Source, "destination": "198.51.100.4:443"}}
	enrichWebhookDomains(rows, o.domainHints(), true)
	if rows[0]["domain"] != "www.example.com" || rows[1]["domain"] != nil {
		t.Fatal("domain joined different target", rows)
	}
	event.Target = "other.example:443"
	o.record(event)
	if _, exists := o.domainHints()[eventKey(event)]; exists {
		t.Fatal("ambiguous domain accepted")
	}
	o.setDomainCollection(false)
	for _, events := range o.events {
		for _, e := range events {
			if e.Target != "" || strings.Contains(e.Destination, "example") {
				t.Fatal("domain cache not cleared")
			}
		}
	}
	enrichWebhookDomains(rows, nil, false)
	if rows[0]["domain"] != "" || !strings.Contains(str(rows[0]["domain_note"]), "已关闭") {
		t.Fatal(rows)
	}
}
