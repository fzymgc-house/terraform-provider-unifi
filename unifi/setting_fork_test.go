package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// A leaf Default fires even when its parent object is absent from the configuration, and a
// value that differs from the controller's trips the plan-change gate of the framework: every
// Computed attribute without UseStateForUnknown is then re-planned as unknown and the resource
// plans an update with nothing to change. The leaves of the sections a configuration need not
// declare therefore carry no static default and read from the controller.
func Test_settingResource_controllerReadSectionsHaveNoLeafDefaults(t *testing.T) {
	r := &settingResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	sections := map[string][]string{
		"auto_speedtest":       {"enabled"},
		"dpi":                  {"enabled", "fingerprinting_enabled"},
		"lcm":                  {"enabled", "sync", "touch_event"},
		"network_optimization": {"enabled"},
	}
	for section, leaves := range sections {
		obj, ok := resp.Schema.Attributes[section].(schema.SingleNestedAttribute)
		if !ok {
			t.Fatalf(
				"%s is %T, want schema.SingleNestedAttribute",
				section,
				resp.Schema.Attributes[section],
			)
		}
		for _, leaf := range leaves {
			b, ok := obj.Attributes[leaf].(schema.BoolAttribute)
			if !ok {
				t.Fatalf(
					"%s.%s is %T, want schema.BoolAttribute",
					section,
					leaf,
					obj.Attributes[leaf],
				)
			}
			if b.Default != nil {
				t.Errorf("%s.%s has a static default %v", section, leaf, b.Default)
			}
			if !b.Optional || !b.Computed {
				t.Errorf(
					"%s.%s Optional=%v Computed=%v, want both true",
					section,
					leaf,
					b.Optional,
					b.Computed,
				)
			}
		}
	}
}
