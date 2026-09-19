package unifi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// devicePortsFixture is shaped like a managed 24-port switch: two LAG lead
// ports, two trunk ports carrying keys go-unifi's DevicePortOverrides does not
// model (stp_edge_state, multicast_router_mode, sd_wan_underlay_port, lag_idx),
// an access port, and a key no release of the provider knows yet.
const devicePortsFixture = `[
 {"port_idx":6,"name":"uplink-a","op_mode":"aggregate","aggregate_members":[6,7],"lag_idx":1,
  "forward":"all","tagged_vlan_mgmt":"auto","native_networkconf_id":"net-core","setting_preference":"manual",
  "autoneg":true,"poe_mode":"auto","port_security_mac_address":[]},
 {"port_idx":12,"name":"trunk-a","forward":"all","tagged_vlan_mgmt":"auto","native_networkconf_id":"net-core",
  "stp_edge_state":"disabled","stp_bpdu_guard_enabled":false,"multicast_router_mode":"NONE",
  "sd_wan_underlay_port":false,"stp_port_mode":true,"dot1x_ctrl":"force_authorized","dot1x_idle_timeout":300,
  "voice_networkconf_id":"","x_future_key":{"nested":[1,2]}},
 {"port_idx":13,"name":"trunk-b","forward":"all","tagged_vlan_mgmt":"auto","native_networkconf_id":"net-core",
  "stp_edge_state":"disabled","multicast_router_mode":"NONE"},
 {"port_idx":9,"name":"access","forward":"native","tagged_vlan_mgmt":"block_all","native_networkconf_id":"net-mgmt",
  "poe_mode":"off"},
 {"port_idx":23,"name":"uplink-b","op_mode":"aggregate","aggregate_members":[23,24],"lag_idx":2,"poe_mode":"off"}
]`

func fixtureEntries(t *testing.T) []portOverrideEntry {
	t.Helper()
	var entries []portOverrideEntry
	if err := json.Unmarshal([]byte(devicePortsFixture), &entries); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return entries
}

func entryByIndex(t *testing.T, entries []portOverrideEntry, idx int64) portOverrideEntry {
	t.Helper()
	for _, e := range entries {
		if i, err := e.index(); err == nil && i == idx {
			return e
		}
	}
	return nil
}

// allDeclared declares every fixture port with no keys, as a configuration
// that lists each port but sets nothing on it would.
func allDeclared(t *testing.T, entries []portOverrideEntry) map[int64]portOverrideEntry {
	t.Helper()
	declared := map[int64]portOverrideEntry{}
	for _, e := range entries {
		idx, err := e.index()
		if err != nil {
			t.Fatal(err)
		}
		declared[idx] = portOverrideEntry{}
	}
	return declared
}

func TestMergePortOverrides_KeepsEveryUndeclaredKey(t *testing.T) {
	live := fixtureEntries(t)
	declared := allDeclared(t, live)
	declared[12] = portOverrideEntry{"name": json.RawMessage(`"trunk-a-renamed"`)}

	merged, err := mergePortOverrides(live, declared)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != len(live) {
		t.Fatalf("merged %d entries, want %d", len(merged), len(live))
	}
	for _, want := range live {
		idx, _ := want.index()
		got := entryByIndex(t, merged, idx)
		if len(got) != len(want) {
			t.Errorf("port %d: %d keys, want %d", idx, len(got), len(want))
		}
		for k, v := range want {
			if idx == 12 && k == "name" {
				continue
			}
			if string(got[k]) != string(v) {
				t.Errorf("port %d key %s = %s, want %s", idx, k, got[k], v)
			}
		}
	}
	if got := string(entryByIndex(t, merged, 12)["name"]); got != `"trunk-a-renamed"` {
		t.Errorf("port 12 name = %s, want the declared value", got)
	}
}

func TestMergePortOverrides_DropsUndeclaredPorts(t *testing.T) {
	live := fixtureEntries(t)
	declared := allDeclared(t, live)
	delete(declared, 13)

	merged, err := mergePortOverrides(live, declared)
	if err != nil {
		t.Fatal(err)
	}
	if entryByIndex(t, merged, 13) != nil {
		t.Error("port 13 is not declared but is still in the merged array")
	}
	if len(merged) != len(live)-1 {
		t.Errorf("merged %d entries, want %d", len(merged), len(live)-1)
	}
}

func TestMergePortOverrides_AppendsNewPortsInIndexOrder(t *testing.T) {
	live := fixtureEntries(t)
	declared := allDeclared(t, live)
	declared[20] = portOverrideEntry{"name": json.RawMessage(`"new-b"`)}
	declared[2] = portOverrideEntry{"name": json.RawMessage(`"new-a"`)}

	merged, err := mergePortOverrides(live, declared)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, e := range merged {
		idx, _ := e.index()
		order = append(order, idx)
	}
	want := []int64{6, 12, 13, 9, 23, 2, 20}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
	got := entryByIndex(t, merged, 2)
	if string(got["port_idx"]) != "2" || string(got["name"]) != `"new-a"` {
		t.Errorf("new port 2 = %v", got)
	}
}

func TestMergePortOverrides_NothingDeclaredWritesAnEmptyArray(t *testing.T) {
	merged, err := mergePortOverrides(fixtureEntries(t), map[int64]portOverrideEntry{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"port_overrides": merged})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"port_overrides":[]}` {
		t.Errorf("body = %s, want an empty array, never null", body)
	}
}

func TestMergePortOverrides_RejectsEntryWithoutIndex(t *testing.T) {
	live := []portOverrideEntry{{"name": json.RawMessage(`"no-index"`)}}
	if _, err := mergePortOverrides(live, map[int64]portOverrideEntry{}); err == nil {
		t.Error("expected an error for an entry without port_idx")
	}
}

func TestPortOverridesToState_ReadsEveryEntry(t *testing.T) {
	ports, diags := portOverridesToState(fixtureEntries(t))
	if diags.HasError() {
		t.Fatal(diags)
	}
	if len(ports.Elements()) != 5 {
		t.Fatalf("%d ports, want 5", len(ports.Elements()))
	}
	lag := portAttrs(t, ports, "6")
	if !lag["op_mode"].Equal(types.StringValue("aggregate")) ||
		!lag["lag_idx"].Equal(types.Int64Value(1)) {
		t.Errorf("port 6 = %v", lag)
	}
	wantMembers, _ := types.SetValue(
		types.Int64Type,
		[]attr.Value{types.Int64Value(7), types.Int64Value(6)},
	)
	if members := lag["aggregate_members"]; !members.Equal(wantMembers) {
		t.Errorf("port 6 aggregate_members = %v, want %v", members, wantMembers)
	}
	trunk := portAttrs(t, ports, "12")
	if !trunk["stp_edge_state"].Equal(types.StringValue("disabled")) ||
		!trunk["dot1x_idle_timeout"].Equal(types.Int64Value(300)) {
		t.Errorf("port 12 = %v", trunk)
	}
	if !trunk["op_mode"].IsNull() || !trunk["port_profile_id"].IsNull() {
		t.Error("keys absent from the entry must read as null")
	}
	if !trunk["voice_networkconf_id"].Equal(types.StringValue("")) {
		t.Error("an empty string must read as an empty string, not null")
	}
}

func portAttrs(t *testing.T, ports types.Map, key string) map[string]attr.Value {
	t.Helper()
	obj, ok := ports.Elements()[key].(types.Object)
	if !ok {
		t.Fatalf("ports[%q] is %T, want an object", key, ports.Elements()[key])
	}
	return obj.Attributes()
}

// portObject builds a ports element with every attribute null except those in set.
func portObject(t *testing.T, set map[string]attr.Value) types.Object {
	t.Helper()
	values := map[string]attr.Value{}
	for _, f := range portFields {
		values[f.name] = nullPortValue(f.kind)
	}
	for k, v := range set {
		values[k] = v
	}
	obj, diags := types.ObjectValue(devicePortAttrTypes(), values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return obj
}

func portMap(t *testing.T, elems map[string]attr.Value) types.Map {
	t.Helper()
	m, diags := types.MapValue(types.ObjectType{AttrTypes: devicePortAttrTypes()}, elems)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return m
}

func TestDeclaredPorts_WritesOnlyConfiguredKeys(t *testing.T) {
	ctx := context.Background()
	excluded, _ := types.SetValue(
		types.StringType,
		[]attr.Value{types.StringValue("net-b"), types.StringValue("net-a")},
	)
	// The plan carries prior-state values for attributes left out of the
	// configuration (UseStateForUnknown); only configured ones may be written.
	plan := portMap(t, map[string]attr.Value{
		"12": portObject(t, map[string]attr.Value{
			"name":                     types.StringValue("trunk-a"),
			"tagged_vlan_mgmt":         types.StringValue("custom"),
			"excluded_networkconf_ids": excluded,
			"autoneg":                  types.BoolValue(true),
			"lag_idx":                  types.Int64Value(4),
		}),
	})
	config := portMap(t, map[string]attr.Value{
		"12": portObject(t, map[string]attr.Value{
			"name":                     types.StringValue("trunk-a"),
			"tagged_vlan_mgmt":         types.StringValue("custom"),
			"excluded_networkconf_ids": excluded,
			"lag_idx":                  types.Int64Value(4),
		}),
	})

	declared, diags := declaredPorts(ctx, plan, config)
	if diags.HasError() {
		t.Fatal(diags)
	}
	got := declared[12]
	want := map[string]string{
		"name":                     `"trunk-a"`,
		"tagged_vlan_mgmt":         `"custom"`,
		"excluded_networkconf_ids": `["net-a","net-b"]`,
	}
	if len(got) != len(want) {
		t.Fatalf("declared keys %v, want %v", got, want)
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

func TestDeclaredPorts_PortWithNoKeysIsStillDeclared(t *testing.T) {
	plan := portMap(t, map[string]attr.Value{"22": portObject(t, nil)})
	declared, diags := declaredPorts(context.Background(), plan, plan)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if entry, ok := declared[22]; !ok || len(entry) != 0 {
		t.Errorf("declared[22] = %v, %v; want an empty entry", entry, ok)
	}
}

func TestDeclaredPorts_RejectsNonIndexKeys(t *testing.T) {
	plan := portMap(t, map[string]attr.Value{"uplink": portObject(t, nil)})
	if _, diags := declaredPorts(context.Background(), plan, plan); !diags.HasError() {
		t.Error("expected an error for a non-numeric ports key")
	}
}

// Some controllers send numbers as JSON strings, and an empty string for an
// unset number; neither may fail the read of the whole device.
func TestPortOverridesToState_ToleratesQuotedNumbers(t *testing.T) {
	var entries []portOverrideEntry
	raw := `[{"port_idx":"4","speed":"1000","dot1x_idle_timeout":"","aggregate_members":["4",5]}]`
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	ports, diags := portOverridesToState(entries)
	if diags.HasError() {
		t.Fatal(diags)
	}
	p := portAttrs(t, ports, "4")
	if !p["speed"].Equal(types.Int64Value(1000)) {
		t.Errorf("speed = %v, want 1000", p["speed"])
	}
	if !p["dot1x_idle_timeout"].IsNull() {
		t.Errorf("dot1x_idle_timeout = %v, want null for an empty string", p["dot1x_idle_timeout"])
	}
	want, _ := types.SetValue(
		types.Int64Type,
		[]attr.Value{types.Int64Value(4), types.Int64Value(5)},
	)
	if !p["aggregate_members"].Equal(want) {
		t.Errorf("aggregate_members = %v, want %v", p["aggregate_members"], want)
	}
}
