package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type wgEditRunner struct {
	Runner
	active, enabled  map[string]bool
	events           []string
	oldPath, oldText string
	t                *testing.T
}

func (r *wgEditRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if len(args) > 2 && args[0] == "systemctl" {
		unit := args[2]
		if args[1] == "show" {
			state, auto := "inactive", "disabled"
			if r.active[unit] {
				state = "active"
			}
			if r.enabled[unit] {
				auto = "enabled"
			}
			return fmt.Sprintf("ActiveState=%s\nSubState=exited\nMainPID=0\nUnitFileState=%s\n", state, auto), nil
		}
		r.events = append(r.events, args[1]+":"+unit)
		if args[1] == "stop" {
			r.active[unit] = false
		}
		if args[1] == "start" {
			r.active[unit] = true
		}
		if args[1] == "enable" {
			r.enabled[unit] = true
		}
		if args[1] == "disable" {
			r.enabled[unit] = false
		}
	}
	return r.Runner.Run(ctx, args, input)
}
func TestWGAllowedInputAcceptsMultiplePrefixes(t *testing.T) {
	for _, input := range []any{"10.0.0.2/32, 192.0.2.0/24\n2001:db8::/64", []any{"10.0.0.2/32,192.0.2.0/24", "2001:db8::/64"}, "10.0.0.2/32，192.0.2.0/24；2001:db8::/64"} {
		values, err := wgAllowedInput(input)
		if err != nil || len(values) != 3 {
			t.Fatal(values, err)
		}
	}
	for _, input := range []any{"default", "192.0.2.0/99", []any{1}, 123} {
		if _, err := wgAllowedInput(input); err == nil {
			t.Fatal("invalid AllowedIPs accepted")
		}
	}
}
func TestWGEditStopsBeforeWritingAndRestoresOriginalFilesAndState(t *testing.T) {
	for _, rename := range []bool{false, true} {
		t.Run(fmt.Sprint(rename), func(t *testing.T) {
			a, f := fixture(t)
			dir := t.TempDir()
			path := filepath.Join(dir, "wg-before.conf")
			original := "[Interface]\nPrivateKey = synthetic-test-value\nAddress = 10.0.0.1/24\nSaveConfig = true\n"
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			r := &wgEditRunner{Runner: f, active: map[string]bool{"wg-quick@wg-before": true}, enabled: map[string]bool{"wg-quick@wg-before": true}, t: t}
			a.runner = r
			name := "wg-before"
			if rename {
				name = "wg-after"
			}
			profile, _ := parseWG("wg-before", original)
			profile.Addresses = []string{"10.0.0.10/24"}
			plan := &NetworkPlan{ID: id(), Kind: "wireguard", Status: "armed"}
			if err := a.network.planWGEditFiles(plan, profile, path, name, []byte(original), true, true); err != nil {
				t.Fatal(err)
			}
			if err := plan.precondition(); err != nil {
				t.Fatal(err)
			}
			if err := plan.applyFiles(); err != nil {
				t.Fatal(err)
			}
			if len(r.events) == 0 || r.events[0] != "stop:wg-quick@wg-before" {
				t.Fatal("original interface not stopped before editing", r.events)
			}
			target := filepath.Join(dir, name+".conf")
			raw, _ := os.ReadFile(target)
			if !strings.Contains(string(raw), "10.0.0.10/24") {
				t.Fatal("new profile not written")
			}
			if rename {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("old configuration survived rename")
				}
			}
			for _, step := range plan.steps {
				if _, err := r.Run(context.Background(), step.Args, step.Input); err != nil {
					t.Fatal(err)
				}
			}
			if err := plan.postcondition(); err != nil {
				t.Fatal(err)
			}
			// Exercise the same durable before-files / files / inverse / after-files order.
			if err := a.network.writeSpec(plan, len(plan.steps)); err != nil {
				t.Fatal(err)
			}
			var spec rollbackSpec
			if err := readJSON(a.network.specPath(plan), &spec); err != nil {
				t.Fatal(err)
			}
			files := spec.Files
			if spec.File != nil {
				files = append(files, spec.File)
			}
			if err := prepareWGRollbackFiles(a.network.specPath(plan), &spec, files); err != nil {
				t.Fatal(err)
			}
			for _, step := range spec.BeforeFile {
				if err := runRollbackStep(r, step); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range files {
				if err := restoreManagedFile(file); err != nil {
					t.Fatal(err)
				}
			}
			slices.Reverse(spec.Undo)
			for _, step := range spec.Undo {
				if err := runRollbackStep(r, step); err != nil {
					t.Fatal(err)
				}
			}
			for _, step := range spec.AfterFile {
				if err := runRollbackStep(r, step); err != nil {
					t.Fatal(err)
				}
			}
			restored, _ := os.ReadFile(path)
			if string(restored) != original || !r.active["wg-quick@wg-before"] || !r.enabled["wg-quick@wg-before"] {
				t.Fatal("original interface state not restored")
			}
			if rename {
				if _, err := os.Stat(target); !os.IsNotExist(err) || r.active["wg-quick@wg-after"] || r.enabled["wg-quick@wg-after"] {
					t.Fatal("renamed interface retained after rollback")
				}
			}
		})
	}
}
func TestWGEditRejectsOccupiedDestination(t *testing.T) {
	a, _ := fixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "wg-before.conf")
	raw := []byte("[Interface]\n")
	_ = os.WriteFile(path, raw, 0600)
	_ = os.WriteFile(filepath.Join(dir, "wg-after.conf"), raw, 0600)
	profile, _ := parseWG("wg-before", string(raw))
	if a.network.planWGEditFiles(&NetworkPlan{ID: id()}, profile, path, "wg-after", raw, false, false) == nil {
		t.Fatal("occupied name accepted")
	}
}
