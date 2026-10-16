package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// wlanPlanWithNullGroups returns a plan whose nested groups and lists are null, the shape an
// update carries for every attribute the configuration leaves out.
func wlanPlanWithNullGroups() wlanFrameworkResourceModel {
	return wlanFrameworkResourceModel{
		MacFilter: types.ObjectNull(map[string]attr.Type{
			"enabled": types.BoolType,
			"list":    types.SetType{ElemType: types.StringType},
			"policy":  types.StringType,
		}),
		PrivatePresharedKeys: types.ListNull(
			types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()},
		),
		ApGroup:             types.ObjectNull(wlanApGroupAttrTypes()),
		WPA:                 types.ObjectNull(wlanWPAAttrTypes()),
		WPA3:                types.ObjectNull(wlanWPA3AttrTypes()),
		Radius:              types.ObjectNull(wlanRadiusAttrTypes()),
		RoamingAssistantNa:  types.ObjectNull(wlanRoamingAssistantAttrTypes()),
		RoamingAssistant6E:  types.ObjectNull(wlanRoamingAssistantAttrTypes()),
		WLANBands:           types.SetNull(types.StringType),
		Schedule:            types.ListNull(types.ObjectType{}),
		BroadcastFilterList: types.SetNull(types.StringType),
	}
}

// The controller replaces the whole WLAN on update. planOntoWLAN lays the plan over the WLAN as
// the controller holds it, so a field the resource never sets keeps its live value, a list the
// plan declares replaces the live list, and a null passphrase keeps the live key.
func Test_wlanFrameworkResource_planOntoWLAN_keepsLiveFields(t *testing.T) {
	ctx := context.Background()
	r := &wlanFrameworkResource{}

	saeSync := int64(5)
	live := &unifi.WLAN{
		ID:                        "6abc",
		Name:                      "old",
		Passphrase:                "live-secret",
		SaeGroups:                 []int64{19, 20},
		SaeSync:                   &saeSync,
		MdnsProxyMode:             "off",
		SettingPreference:         "manual",
		ApGroupIDs:                []string{"group-live"},
		BroadcastFilterList:       []string{"aa:bb:cc:dd:ee:ff"},
		RADIUSMACaclFormat:        "none",
		MinrateNgAdvertisingRates: true,
	}

	plan := wlanPlanWithNullGroups()
	plan.ID = types.StringValue("6abc")
	plan.Name = types.StringValue("new")
	plan.Security = types.StringValue("wpapsk")
	plan.ApGroup = types.ObjectValueMust(wlanApGroupAttrTypes(), map[string]attr.Value{
		"ids":  types.SetValueMust(types.StringType, []attr.Value{types.StringValue("group-plan")}),
		"mode": types.StringValue("groups"),
	})

	got, diags := r.planOntoWLAN(ctx, plan, live)
	if diags.HasError() {
		t.Fatalf("planOntoWLAN() diagnostics: %v", diags)
	}
	if got.Name != "new" {
		t.Errorf("Name = %q, want the planned name", got.Name)
	}
	if got.Passphrase != "live-secret" {
		t.Errorf("Passphrase = %q, want the live key kept for a null plan", got.Passphrase)
	}
	if len(got.SaeGroups) != 2 || got.SaeSync == nil || *got.SaeSync != 5 {
		t.Errorf("SaeGroups = %v, SaeSync = %v, want the live SAE parameters", got.SaeGroups, got.SaeSync)
	}
	if got.MdnsProxyMode != "off" || got.SettingPreference != "manual" ||
		got.RADIUSMACaclFormat != "none" || !got.MinrateNgAdvertisingRates {
		t.Errorf("an undeclared field lost its live value: %+v", got)
	}
	if len(got.ApGroupIDs) != 1 || got.ApGroupIDs[0] != "group-plan" {
		t.Errorf("ApGroupIDs = %v, want the planned list to replace the live one", got.ApGroupIDs)
	}
	if len(got.BroadcastFilterList) != 1 {
		t.Errorf("BroadcastFilterList = %v, want the live list kept for a null plan", got.BroadcastFilterList)
	}
	if live.Name != "old" || len(live.ApGroupIDs) != 1 || live.ApGroupIDs[0] != "group-live" {
		t.Errorf("planOntoWLAN changed its base: %+v", live)
	}

	fresh, diags := r.planOntoWLAN(ctx, plan, nil)
	if diags.HasError() {
		t.Fatalf("planOntoWLAN(nil) diagnostics: %v", diags)
	}
	if fresh.Passphrase != "" || fresh.SaeGroups != nil {
		t.Errorf("a create must start from an empty WLAN: %+v", fresh)
	}
}

// passphrase_wo_version is the only thing that makes a rotated write-only passphrase reach
// the controller: Terraform never diffs a write-only value, so the operator changes the
// version and the resulting update sends passphrase_wo again. The number is a marker with no
// controller side, so it must be a plain optional attribute that follows the plan exactly,
// including back to null.
func Test_wlanFrameworkResource_passphraseWOVersion(t *testing.T) {
	ctx := context.Background()
	r := &wlanFrameworkResource{}

	resp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, resp)
	a, ok := resp.Schema.Attributes["passphrase_wo_version"]
	if !ok {
		t.Fatal("Schema missing attribute passphrase_wo_version")
	}
	if !a.IsOptional() || a.IsComputed() || a.IsSensitive() || a.IsRequired() {
		t.Errorf("passphrase_wo_version must be optional only: %+v", a)
	}

	state := wlanFrameworkResourceModel{PassphraseWOVersion: types.Int64Null()}
	plan := wlanFrameworkResourceModel{PassphraseWOVersion: types.Int64Value(2)}
	r.applyPlanToState(ctx, &plan, &state)
	if state.PassphraseWOVersion.ValueInt64() != 2 {
		t.Errorf("PassphraseWOVersion = %v, want 2 from the plan", state.PassphraseWOVersion)
	}

	plan.PassphraseWOVersion = types.Int64Null()
	r.applyPlanToState(ctx, &plan, &state)
	if !state.PassphraseWOVersion.IsNull() {
		t.Errorf("PassphraseWOVersion = %v, want null once the config drops it", state.PassphraseWOVersion)
	}
}

// A WLAN the controller UI created can hold null in group_rekey and "allow" in
// mac_filter.policy. A schema default on either attribute makes the framework plan a value
// that differs from the prior state whenever the config leaves it out, so an imported WLAN
// never reaches a plan with nothing to change, and a nested default fires even when the
// whole mac_filter object is absent. Both attributes stay computed without a default, and
// an unknown group_rekey is not sent, because an unknown Int64 converts to a pointer to zero.
func Test_wlanFrameworkResource_importDefaults(t *testing.T) {
	ctx := context.Background()
	r := &wlanFrameworkResource{}

	resp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, resp)

	groupRekey, ok := resp.Schema.Attributes["group_rekey"].(fwschema.Int64Attribute)
	if !ok {
		t.Fatal("group_rekey is not an Int64Attribute")
	}
	if groupRekey.Default != nil || !groupRekey.IsComputed() {
		t.Errorf("group_rekey must be computed with no default: %+v", groupRekey)
	}

	macFilter, ok := resp.Schema.Attributes["mac_filter"].(fwschema.SingleNestedAttribute)
	if !ok {
		t.Fatal("mac_filter is not a SingleNestedAttribute")
	}
	policy, ok := macFilter.Attributes["policy"].(fwschema.StringAttribute)
	if !ok {
		t.Fatal("mac_filter.policy is not a StringAttribute")
	}
	if policy.Default != nil || !policy.IsComputed() {
		t.Errorf("mac_filter.policy must be computed with no default: %+v", policy)
	}

	rekey := int64(600)
	live := &unifi.WLAN{GroupRekey: &rekey}
	plan := wlanPlanWithNullGroups()
	plan.Name = types.StringValue("w")
	plan.Security = types.StringValue("wpapsk")
	plan.GroupRekey = types.Int64Unknown()
	got, diags := r.planOntoWLAN(ctx, plan, live)
	if diags.HasError() {
		t.Fatalf("planOntoWLAN() diagnostics: %v", diags)
	}
	if got.GroupRekey == nil || *got.GroupRekey != 600 {
		t.Errorf("GroupRekey = %v, want the live 600 kept for an unknown plan", got.GroupRekey)
	}
	fresh, _ := r.planOntoWLAN(ctx, plan, nil)
	if fresh.GroupRekey != nil {
		t.Errorf("GroupRekey = %v, want nil on create for an unknown plan", *fresh.GroupRekey)
	}
}

// rrm_enabled is managed only when stated. A plan that leaves it unset keeps the live value on
// an update, a plan that states it writes it, and a read maps the live value to the model.
func Test_wlanFrameworkResource_rrmEnabled(t *testing.T) {
	ctx := context.Background()
	r := &wlanFrameworkResource{}

	live := &unifi.WLAN{ID: "6abc", RrmEnabled: true}
	unset := wlanPlanWithNullGroups()
	unset.ID = types.StringValue("6abc")
	unset.Name = types.StringValue("w")
	unset.Security = types.StringValue("wpapsk")

	got, diags := r.planOntoWLAN(ctx, unset, live)
	if diags.HasError() {
		t.Fatalf("planOntoWLAN() diagnostics: %v", diags)
	}
	if !got.RrmEnabled {
		t.Errorf("an unset rrm_enabled lost its live value: %+v", got)
	}

	stated := unset
	stated.RrmEnabled = types.BoolValue(false)
	got, diags = r.planOntoWLAN(ctx, stated, live)
	if diags.HasError() {
		t.Fatalf("planOntoWLAN() diagnostics: %v", diags)
	}
	if got.RrmEnabled {
		t.Errorf("a stated rrm_enabled = false was not written: %+v", got)
	}

	var model wlanFrameworkResourceModel
	model.MacFilter = unset.MacFilter
	model.PrivatePresharedKeys = unset.PrivatePresharedKeys
	if diags := r.wlanToModel(ctx, live, &model, "default"); diags.HasError() {
		t.Fatalf("wlanToModel() diagnostics: %v", diags)
	}
	if !model.RrmEnabled.ValueBool() {
		t.Errorf("the live rrm_enabled did not reach the model: %+v", model)
	}
}
