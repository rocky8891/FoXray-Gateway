package gateway

import (
	"slices"
	"strings"
	"testing"
)

func TestWarpHostPolicyRestrictsRoutingAndMigratesLegacyRules(t *testing.T) {
	const legacy = `[
   {"priority":16069,"src":"172.18.30.166","dst":"172.22.0.119","table":"main"},
   {"priority":16070,"src":"all","table":"main","suppress_prefixlen":0},
   {"priority":16079,"not":null,"src":"all","fwmark":"0x100cf","table":"65743"}
 ]`
	cases := []struct {
		name, current string
		want          []string
		wantErr       bool
	}{
		{"legacy", legacy, []string{
			"ip -4 rule add pref 16078 oif CloudflareWARP lookup 65743",
			"ip -4 rule del pref 16079 from all to all not fwmark 0x100cf/0xffffffff lookup 65743",
			"ip -4 rule del pref 16070 from all to all lookup main suppress_prefixlength 0",
		}, false},
		{"already migrated", `[{"priority":16078,"src":"all","oif":"CloudflareWARP","table":"65743"}]`, nil, false},
		{"ambiguous legacy priority", `[{"priority":16079,"src":"172.18.30.166","table":"65743"},{"priority":16079,"not":null,"src":"all","fwmark":"0x100cf","table":"65743"}]`, []string{"ip -4 rule add pref 16078 oif CloudflareWARP lookup 65743"}, false},
		{"new priority occupied", `[{"priority":16078,"not":null,"oif":"CloudflareWARP","table":"65743"}]`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var changes []string
			err := configureWarpHostPolicy(func(args []string, _ string) (string, error) {
				cmd := strings.Join(args, " ")
				if cmd == "ip -j -4 rule" {
					return tc.current, nil
				}
				changes = append(changes, cmd)
				return "", nil
			})
			if (err != nil) != tc.wantErr || !slices.Equal(changes, tc.want) {
				t.Fatalf("changes %v, error %v", changes, err)
			}
		})
	}
}
