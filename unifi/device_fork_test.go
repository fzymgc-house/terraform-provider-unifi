package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// The deprecated assisted_roaming_* leaves no longer reach the controller, and Read carries
// their prior state forward. A configuration that leaves them out must plan the prior value,
// not null, or a radio table adopted before the deprecation plans an update on every run.
func Test_deviceResource_deprecatedAssistedRoamingKeepsState(t *testing.T) {
	r := &deviceResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	radios, ok := resp.Schema.Attributes["radio_table"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf(
			"radio_table is %T, want schema.ListNestedAttribute",
			resp.Schema.Attributes["radio_table"],
		)
	}
	enabled, ok := radios.NestedObject.Attributes["assisted_roaming_enabled"].(schema.BoolAttribute)
	if !ok {
		t.Fatalf(
			"assisted_roaming_enabled is %T",
			radios.NestedObject.Attributes["assisted_roaming_enabled"],
		)
	}
	if !enabled.Computed || len(enabled.PlanModifiers) == 0 {
		t.Errorf(
			"assisted_roaming_enabled Computed=%v modifiers=%d, want computed with UseStateForUnknown",
			enabled.Computed,
			len(enabled.PlanModifiers),
		)
	}
	rssi, ok := radios.NestedObject.Attributes["assisted_roaming_rssi"].(schema.Int64Attribute)
	if !ok {
		t.Fatalf(
			"assisted_roaming_rssi is %T",
			radios.NestedObject.Attributes["assisted_roaming_rssi"],
		)
	}
	if !rssi.Computed || len(rssi.PlanModifiers) == 0 {
		t.Errorf(
			"assisted_roaming_rssi Computed=%v modifiers=%d, want computed with UseStateForUnknown",
			rssi.Computed,
			len(rssi.PlanModifiers),
		)
	}
}
