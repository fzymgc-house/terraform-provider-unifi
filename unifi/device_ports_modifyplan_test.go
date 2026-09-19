package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// runModifyPlan feeds ModifyPlan a prior state, a configuration, and the plan
// the framework would hand it (config values, prior values elsewhere), and
// returns the planned ports.
func runModifyPlan(t *testing.T, state, config, plan map[string]attr.Value) map[string]attr.Value {
	t.Helper()
	ctx := context.Background()
	r := &devicePortsResource{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)

	raw := func(ports map[string]attr.Value) tftypes.Value {
		s := tfsdk.State{
			Schema: sch.Schema,
			Raw:    tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil),
		}
		m := devicePortsResourceModel{
			ID:    types.StringValue("dev1"),
			MAC:   hwtypes.NewMACAddressValue("00:27:22:00:00:05"),
			Site:  types.StringValue("default"),
			Ports: portMap(t, ports),
		}
		if d := s.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		return s.Raw
	}

	req := resource.ModifyPlanRequest{
		State:  tfsdk.State{Schema: sch.Schema, Raw: raw(state)},
		Config: tfsdk.Config{Schema: sch.Schema, Raw: raw(config)},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: raw(plan)},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var out devicePortsResourceModel
	if d := resp.Plan.Get(ctx, &out); d.HasError() {
		t.Fatal(d)
	}
	return out.Ports.Elements()
}

func portAttrsOf(t *testing.T, v attr.Value) map[string]attr.Value {
	t.Helper()
	obj, ok := v.(types.Object)
	if !ok {
		t.Fatalf("%T is not an object", v)
	}
	return obj.Attributes()
}

func TestModifyPlan_UnchangedPortKeepsPriorValues(t *testing.T) {
	lag := map[string]attr.Value{
		"name":     types.StringValue("uplink"),
		"op_mode":  types.StringValue("aggregate"),
		"poe_mode": types.StringValue("off"),
		"lag_idx":  types.Int64Value(2),
	}
	trunkState := map[string]attr.Value{
		"name":     types.StringValue("trunk"),
		"poe_mode": types.StringValue("auto"),
	}
	trunkPlan := map[string]attr.Value{
		"name":     types.StringValue("trunk-renamed"),
		"poe_mode": types.StringValue("auto"),
	}
	state := map[string]attr.Value{"6": portObject(t, lag), "12": portObject(t, trunkState)}
	config := map[string]attr.Value{
		"6": portObject(
			t,
			map[string]attr.Value{
				"name":    types.StringValue("uplink"),
				"op_mode": types.StringValue("aggregate"),
			},
		),
		"12": portObject(t, map[string]attr.Value{"name": types.StringValue("trunk-renamed")}),
	}
	plan := map[string]attr.Value{"6": portObject(t, lag), "12": portObject(t, trunkPlan)}

	got := runModifyPlan(t, state, config, plan)

	unchanged := portAttrsOf(t, got["6"])
	for name, want := range lag {
		if !unchanged[name].Equal(want) {
			t.Errorf("unchanged port 6 %s = %v, want the prior %v", name, unchanged[name], want)
		}
	}
	changed := portAttrsOf(t, got["12"])
	if !changed["name"].Equal(types.StringValue("trunk-renamed")) {
		t.Errorf("changed port 12 name = %v, want the declared value", changed["name"])
	}
	if !changed["poe_mode"].IsUnknown() || !changed["lag_idx"].IsUnknown() {
		t.Errorf(
			"changed port 12 undeclared attributes must be unknown, got poe_mode=%v lag_idx=%v",
			changed["poe_mode"],
			changed["lag_idx"],
		)
	}
}

func TestModifyPlan_NewPortLeavesUndeclaredUnknown(t *testing.T) {
	existing := map[string]attr.Value{"name": types.StringValue("trunk")}
	state := map[string]attr.Value{"12": portObject(t, existing)}
	newPort := map[string]attr.Value{"name": types.StringValue("new")}
	config := map[string]attr.Value{"12": portObject(t, existing), "3": portObject(t, newPort)}
	plan := map[string]attr.Value{"12": portObject(t, existing), "3": portObject(t, newPort)}

	got := runModifyPlan(t, state, config, plan)

	p := portAttrsOf(t, got["3"])
	if !p["name"].Equal(types.StringValue("new")) || !p["tagged_vlan_mgmt"].IsUnknown() {
		t.Errorf(
			"new port 3 = name %v tagged_vlan_mgmt %v; want the declared name and an unknown rest",
			p["name"],
			p["tagged_vlan_mgmt"],
		)
	}
	if !portAttrsOf(t, got["12"])["tagged_vlan_mgmt"].IsNull() {
		t.Error("unchanged port 12 must keep its prior null, not become unknown")
	}
}

func TestModifyPlan_UnknownConfigValueCountsAsChanged(t *testing.T) {
	prior := map[string]attr.Value{
		"native_networkconf_id": types.StringValue("net-a"),
		"poe_mode":              types.StringValue("auto"),
	}
	pending := map[string]attr.Value{"native_networkconf_id": types.StringUnknown()}
	state := map[string]attr.Value{"9": portObject(t, prior)}
	config := map[string]attr.Value{"9": portObject(t, pending)}
	plan := map[string]attr.Value{"9": portObject(t, map[string]attr.Value{
		"native_networkconf_id": types.StringUnknown(),
		"poe_mode":              types.StringValue("auto"),
	})}

	got := runModifyPlan(t, state, config, plan)

	if p := portAttrsOf(t, got["9"]); !p["poe_mode"].IsUnknown() {
		t.Errorf(
			"port 9 poe_mode = %v; a port with an unknown declared value is changed",
			p["poe_mode"],
		)
	}
}
