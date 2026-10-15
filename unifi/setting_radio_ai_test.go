package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// The controller replaces the whole radio_ai section on write, so a model that turns the
// optimizer off and states nothing else must keep every other field of the current section,
// the channel blacklist and the per-radio widths among them, and auto_enabled as it was.
func Test_settingResource_radioAiModelToSetting_keepsUnsetFields(t *testing.T) {
	r := &settingResource{}
	var diags diag.Diagnostics
	ch, width := int64(2), int64(20)
	base := &settings.RadioAi{
		Enabled:                     true,
		AutoEnabled:                 false,
		Default:                     true,
		Optimize:                    []string{"channel"},
		Radios:                      []string{"ng", "na", "6e"},
		CronExpr:                    "0 3 * * *",
		SettingPreference:           "auto",
		AutoChannelPresetsType:      "maximum_speed",
		AutoAdjustChannelsToCountry: true,
		ChannelsNg:                  []int64{1, 6, 11},
		HtModesNg:                   []int64{20},
		ChannelsBlacklist: []settings.SettingRadioAiChannelsBlacklist{
			{Channel: &ch, ChannelWidth: &width, Radio: "ng"},
		},
		RadiosConfiguration: []settings.SettingRadioAiRadiosConfiguration{{Radio: "ng"}},
	}
	model := &settingRadioAiModel{
		Enabled:                     types.BoolValue(false),
		AutoEnabled:                 types.BoolNull(),
		Default:                     types.BoolUnknown(),
		Optimize:                    types.ListNull(types.StringType),
		Radios:                      types.ListNull(types.StringType),
		CronExpr:                    types.StringNull(),
		SettingPreference:           types.StringNull(),
		AutoChannelPresetsType:      types.StringNull(),
		AutoAdjustChannelsToCountry: types.BoolNull(),
		ChannelsNg: types.ListValueMust(
			types.Int64Type,
			[]attr.Value{types.Int64Value(1)},
		),
		ChannelsNa:          types.ListNull(types.Int64Type),
		Channels6E:          types.ListNull(types.Int64Type),
		HtModesNg:           types.ListNull(types.Int64Type),
		HtModesNa:           types.ListNull(types.Int64Type),
		ExcludeDevices:      types.ListNull(types.StringType),
		HighPriorityDevices: types.ListNull(types.StringType),
	}
	got := r.radioAiModelToSetting(context.Background(), model, base, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got.Enabled {
		t.Error("Enabled not set from the model")
	}
	if got.AutoEnabled || !got.Default || got.CronExpr != "0 3 * * *" || len(got.Radios) != 3 {
		t.Errorf(
			"unset fields changed: auto=%v default=%v cron=%q radios=%v",
			got.AutoEnabled,
			got.Default,
			got.CronExpr,
			got.Radios,
		)
	}
	if len(got.ChannelsNg) != 1 || got.ChannelsNg[0] != 1 {
		t.Errorf("ChannelsNg = %v, want the planned list", got.ChannelsNg)
	}
	if len(got.ChannelsBlacklist) != 1 || len(got.RadiosConfiguration) != 1 {
		t.Errorf(
			"a field the block does not expose was lost: blacklist=%v radios=%v",
			got.ChannelsBlacklist,
			got.RadiosConfiguration,
		)
	}
}

// A read maps every list the controller returns, and an absent list reads as an empty list
// rather than null, so a configuration that states none plans no change.
func Test_settingResource_radioAiSettingToModel(t *testing.T) {
	r := &settingResource{}
	var diags diag.Diagnostics
	setting := &settings.RadioAi{
		Enabled:    true,
		CronExpr:   "0 3 * * *",
		ChannelsNg: []int64{1, 6, 11},
		Radios:     []string{"ng"},
	}
	got := r.radioAiSettingToModel(context.Background(), setting, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !got.Enabled.ValueBool() || got.CronExpr.ValueString() != "0 3 * * *" {
		t.Errorf("Enabled = %v, CronExpr = %v", got.Enabled, got.CronExpr)
	}
	if len(got.ChannelsNg.Elements()) != 3 || len(got.Radios.Elements()) != 1 {
		t.Errorf("ChannelsNg = %v, Radios = %v", got.ChannelsNg, got.Radios)
	}
	if got.ExcludeDevices.IsNull() || len(got.ExcludeDevices.Elements()) != 0 {
		t.Errorf("ExcludeDevices = %v, want an empty list", got.ExcludeDevices)
	}
	obj, d := types.ObjectValueFrom(context.Background(), radioAiAttrTypes, got)
	if d.HasError() || obj.IsNull() {
		t.Fatalf("the model does not match radioAiAttrTypes: %v", d)
	}
}
