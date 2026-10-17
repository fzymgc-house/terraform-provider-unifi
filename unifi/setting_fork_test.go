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

// Terraform core keeps the prior value of an optional and computed nested object that the
// configuration leaves out, unless the prior object holds a non-null leaf that the schema does
// not mark computed. Core then infers that the configuration must have set the object before
// and plans it from the configuration, which is null, so the plan-change gate trips on every run
// for a site whose controller fills such a leaf (`country.code` on every site). Every leaf below
// a computed nested object is therefore computed too.
func Test_settingResource_leavesUnderComputedObjectsAreComputed(t *testing.T) {
	r := &settingResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	var walk func(prefix string, attrs map[string]schema.Attribute, underComputed bool)
	walk = func(prefix string, attrs map[string]schema.Attribute, underComputed bool) {
		for name, a := range attrs {
			p := prefix + name
			// The controller never returns the SSH password, so Read leaves the
			// leaf null and the inference of core cannot fire on it.
			if p == "mgmt.ssh.password" {
				continue
			}
			if underComputed && !a.IsComputed() {
				t.Errorf("%s is not computed under a computed object", p)
			}
			switch n := a.(type) {
			case schema.SingleNestedAttribute:
				walk(p+".", n.Attributes, underComputed || n.IsComputed())
			case schema.ListNestedAttribute:
				walk(p+".", n.NestedObject.Attributes, underComputed || n.IsComputed())
			case schema.SetNestedAttribute:
				walk(p+".", n.NestedObject.Attributes, underComputed || n.IsComputed())
			case schema.MapNestedAttribute:
				walk(p+".", n.NestedObject.Attributes, underComputed || n.IsComputed())
			}
		}
	}
	walk("", resp.Schema.Attributes, false)
}
