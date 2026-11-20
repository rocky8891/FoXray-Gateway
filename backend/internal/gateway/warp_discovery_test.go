package gateway

import (
	"net"
	"testing"
)

func TestWarpDiscoveryExpandsUniquePoolAndPreservesCurrent(t *testing.T) {
	rows := discoveryTargets("masque-h2", 443, "192.0.2.1:443", []any{M{"endpoint": "162.159.198.87:443"}, M{"endpoint": "bad"}, M{"endpoint": "162.159.198.87:443"}, M{"endpoint": "162.159.198.88:500"}}, 15)
	if len(rows) != 64 {
		t.Fatal("bounded larger sample missing", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !endpointValid(r) || seen[r] {
			t.Fatal("invalid or duplicate", r)
		}
		seen[r] = true
		_, port, _ := net.SplitHostPort(r)
		if port != "443" {
			t.Fatal(r)
		}
	}
	if !seen["162.159.198.87:443"] {
		t.Fatal("existing candidate discarded")
	}
	other := discoveryTargets("masque-h2", 443, "", nil, 16)
	if rows[5] == other[5] {
		t.Fatal("sampling did not rotate")
	}
}

func TestWarpQualityRejectsLegacyAndIncompleteResults(t *testing.T) {
	row := M{"durable": true, "rtt_ms": 40., "loss_pct": 0.}
	if scoutQuality(row) != -1 {
		t.Fatal("legacy probe result ranked as fresh")
	}
	row["probe_profile"] = scoutProbeProfile
	good := scoutQuality(row)
	row["loss_pct"] = 40.
	if scoutQuality(row) >= good {
		t.Fatal("loss not penalized")
	}
	row["durable"] = false
	if scoutQuality(row) != -1 {
		t.Fatal("failed candidate ranked")
	}
}

func TestOfficialWarpFailureCooldown(t *testing.T) {
	if !officialWarpCoolingDown(M{"official_ok": false, "official_at": now()}) {
		t.Fatal("recent failed endpoint not cooled down")
	}
	if officialWarpCoolingDown(M{"official_ok": false, "official_at": now() - 901}) {
		t.Fatal("cooldown did not expire")
	}
	if officialWarpCoolingDown(M{"official_ok": true, "official_at": now()}) || officialWarpCoolingDown(M{}) {
		t.Fatal("successful or unknown endpoint excluded")
	}
}

func TestActualWarpVerificationRequiresOutageConfirmation(t *testing.T) {
	a, f := fixture(t)
	a.components["warp"] = true
	if _, err := a.warpAction("/api/warp/scout", M{"operation": "verify-best", "protocol": "masque-h2"}); err == nil {
		t.Fatal("disruptive verification accepted without confirmation")
	}
	if len(f.calls) != 0 {
		t.Fatal("unconfirmed verification touched system")
	}
}
