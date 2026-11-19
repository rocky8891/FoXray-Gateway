//go:build linux

package gateway

import (
	"strings"
	"testing"
)

func TestKernelCustomVPNExclusionsLoadInIsolatedNamespace(t *testing.T) {
	a, _ := kernelFixture(t)
	c := clone(a.cfg)
	c["vpn_exclusions"] = M{"mode": "custom", "interfaces": []any{"test0"}, "cidrs": []any{"198.51.100.0/24"}, "domains": []any{"domain:example.org"}}
	script, err := a.captureRulesForEngine(c, M{}, "0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, `iifname { "test0" } return`) || !strings.Contains(script, "198.51.100.0/24") || !strings.Contains(script, `"vone0"`) {
		t.Fatal("custom exclusions removed baseline or failed to compile")
	}
	if _, err := a.command(5, script, "nft", "-c", "-f", "-"); err != nil {
		t.Fatal(err)
	}
}
