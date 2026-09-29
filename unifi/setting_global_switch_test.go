package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// The controller replaces the whole global_switch section on write, so a model that sets only
// dhcp_snoop must keep every other field of the current section, pointers included.
func Test_settingResource_globalSwitchModelToSetting_keepsUnsetFields(t *testing.T) {
	r := &settingResource{}
	var diags diag.Diagnostics
	edge, debounce, poe := false, int64(0), int64(200)
	base := &settings.GlobalSwitch{
		DHCPSnoop:                   false,
		StpVersion:                  "rstp",
		FlowctrlEnabled:             true,
		AutoStpEdgeDetectionEnabled: &edge,
		LinkDebounce:                &debounce,
		PoeStagingDelayMsec:         &poe,
		SwitchExclusions:            []string{"f4:e2:c6:ad:b2:48"},
	}
	model := &settingGlobalSwitchModel{
		DHCPSnoop:                   types.BoolValue(true),
		Dot1XPortctrlEnabled:        types.BoolNull(),
		Dot1XFallbackNetworkID:      types.StringNull(),
		RADIUSProfileID:             types.StringNull(),
		FlowctrlEnabled:             types.BoolUnknown(),
		JumboframeEnabled:           types.BoolNull(),
		StpVersion:                  types.StringNull(),
		AutoStpEdgeDetectionEnabled: types.BoolNull(),
		LinkDebounce:                types.Int64Null(),
		PoeStagingDelayMsec:         types.Int64Value(500),
		SwitchExclusions:            types.ListNull(types.StringType),
	}
	got := r.globalSwitchModelToSetting(context.Background(), model, base, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !got.DHCPSnoop {
		t.Error("DHCPSnoop not set from the model")
	}
	if got.StpVersion != "rstp" || !got.FlowctrlEnabled || len(got.SwitchExclusions) != 1 {
		t.Errorf(
			"unset fields changed: stp=%q flowctrl=%v exclusions=%v",
			got.StpVersion,
			got.FlowctrlEnabled,
			got.SwitchExclusions,
		)
	}
	if got.AutoStpEdgeDetectionEnabled == nil || *got.AutoStpEdgeDetectionEnabled ||
		got.LinkDebounce == nil ||
		*got.LinkDebounce != 0 {
		t.Errorf(
			"pointer fields changed: edge=%v debounce=%v",
			got.AutoStpEdgeDetectionEnabled,
			got.LinkDebounce,
		)
	}
	if got.PoeStagingDelayMsec == nil || *got.PoeStagingDelayMsec != 500 {
		t.Errorf("PoeStagingDelayMsec = %v, want 500", got.PoeStagingDelayMsec)
	}
}

func Test_settingResource_globalSwitchSettingToModel(t *testing.T) {
	r := &settingResource{}
	var diags diag.Diagnostics
	poe := int64(200)
	model := r.globalSwitchSettingToModel(
		context.Background(),
		&settings.GlobalSwitch{DHCPSnoop: true, StpVersion: "rstp", PoeStagingDelayMsec: &poe},
		&diags,
	)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !model.DHCPSnoop.ValueBool() || model.StpVersion.ValueString() != "rstp" ||
		model.PoeStagingDelayMsec.ValueInt64() != 200 {
		t.Errorf("model = %+v", model)
	}
	if !model.AutoStpEdgeDetectionEnabled.IsNull() || !model.LinkDebounce.IsNull() {
		t.Errorf(
			"absent pointer fields must read as null: edge=%v debounce=%v",
			model.AutoStpEdgeDetectionEnabled,
			model.LinkDebounce,
		)
	}
	if _, d := types.ObjectValueFrom(
		context.Background(),
		globalSwitchAttrTypes,
		model,
	); d.HasError() {
		t.Errorf("model does not match globalSwitchAttrTypes: %v", d)
	}
	var _ attr.Value = model.SwitchExclusions
}
