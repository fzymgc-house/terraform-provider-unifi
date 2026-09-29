package unifi

import (
	"encoding/json"
	"testing"
)

// The controller keeps an entry for port 2, a member of the group led by port 1. The resource
// cannot declare port 2, so the read must leave it out or an import never plans zero changes.
func TestPortOverridesToState_LeavesOutLagMemberEntries(t *testing.T) {
	raw := `[
		{"port_idx": 1, "name": "switch-main", "op_mode": "aggregate", "aggregate_members": [1, 2]},
		{"port_idx": 2, "name": " ", "op_mode": "switch"},
		{"port_idx": 3, "name": "nas1", "op_mode": "aggregate", "aggregate_members": [3, 4]},
		{"port_idx": 4, "name": "nas2", "portconf_id": "605552769c9b7d0ab22e4549"},
		{"port_idx": 5, "name": "switch-core"}
	]`
	var entries []portOverrideEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	ports, diags := portOverridesToState(entries)
	if diags.HasError() {
		t.Fatal(diags)
	}
	got := ports.Elements()
	for _, want := range []string{"1", "3", "5"} {
		if _, ok := got[want]; !ok {
			t.Errorf("port %s missing from state", want)
		}
	}
	for _, member := range []string{"2", "4"} {
		if _, ok := got[member]; ok {
			t.Errorf("member port %s must not be in state", member)
		}
	}
	if len(got) != 3 {
		t.Errorf("%d ports in state, want 3", len(got))
	}
}
