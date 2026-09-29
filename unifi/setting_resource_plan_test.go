package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// settingState returns the state of an existing unifi_setting. When section
// is not empty, that section exists in the state with every child null except
// child, which is set to value when value is not nil.
func settingState(
	t *testing.T,
	sch schema.Schema,
	section, child string,
	value attr.Value,
) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	s := tfsdk.State{
		Schema: sch,
		Raw:    tftypes.NewValue(sch.Type().TerraformType(ctx), nil),
	}
	if d := s.SetAttribute(ctx, path.Root("id"), types.StringValue("default")); d.HasError() {
		t.Fatal(d)
	}
	if section == "" {
		return s
	}
	sec, ok := sch.Attributes[section].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("%s is not a single nested attribute", section)
	}
	nulls := map[string]attr.Value{}
	attrTypes := map[string]attr.Type{}
	for name, a := range sec.Attributes {
		at := a.GetType()
		attrTypes[name] = at
		v, err := at.ValueFromTerraform(ctx, tftypes.NewValue(at.TerraformType(ctx), nil))
		if err != nil {
			t.Fatal(err)
		}
		nulls[name] = v
	}
	if value != nil {
		nulls[child] = value
	}
	obj, d := types.ObjectValue(attrTypes, nulls)
	if d.HasError() {
		t.Fatal(d)
	}
	if d := s.SetAttribute(ctx, path.Root(section), obj); d.HasError() {
		t.Fatal(d)
	}
	return s
}

// planChild runs the plan modifiers of section.child with an unknown plan
// value and a null configuration value, as the framework does for a computed
// attribute the configuration leaves unset, and returns the planned value.
func planChild(t *testing.T, a schema.Attribute, state tfsdk.State, p path.Path) attr.Value {
	t.Helper()
	ctx := context.Background()
	switch sa := a.(type) {
	case schema.BoolAttribute:
		var prior types.Bool
		state.GetAttribute(ctx, p, &prior)
		req := planmodifier.BoolRequest{
			Path: p, State: state, StateValue: prior,
			PlanValue: types.BoolUnknown(), ConfigValue: types.BoolNull(),
		}
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		for _, m := range sa.PlanModifiers {
			m.PlanModifyBool(ctx, req, resp)
		}
		return resp.PlanValue
	case schema.Int64Attribute:
		var prior types.Int64
		state.GetAttribute(ctx, p, &prior)
		req := planmodifier.Int64Request{
			Path: p, State: state, StateValue: prior,
			PlanValue: types.Int64Unknown(), ConfigValue: types.Int64Null(),
		}
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}
		for _, m := range sa.PlanModifiers {
			m.PlanModifyInt64(ctx, req, resp)
		}
		return resp.PlanValue
	case schema.StringAttribute:
		var prior types.String
		state.GetAttribute(ctx, p, &prior)
		req := planmodifier.StringRequest{
			Path: p, State: state, StateValue: prior,
			PlanValue: types.StringUnknown(), ConfigValue: types.StringNull(),
		}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		for _, m := range sa.PlanModifiers {
			m.PlanModifyString(ctx, req, resp)
		}
		return resp.PlanValue
	case schema.ListAttribute:
		var prior types.List
		state.GetAttribute(ctx, p, &prior)
		req := planmodifier.ListRequest{
			Path:       p,
			State:      state,
			StateValue: prior,
			PlanValue: types.ListUnknown(
				sa.ElementType,
			),
			ConfigValue: types.ListNull(sa.ElementType),
		}
		resp := &planmodifier.ListResponse{PlanValue: req.PlanValue}
		for _, m := range sa.PlanModifiers {
			m.PlanModifyList(ctx, req, resp)
		}
		return resp.PlanValue
	case schema.ListNestedAttribute:
		lt, ok := sa.GetType().(types.ListType)
		if !ok {
			t.Fatalf("%s is %T, want a list type", p, sa.GetType())
		}
		elem := lt.ElemType
		var prior types.List
		state.GetAttribute(ctx, p, &prior)
		req := planmodifier.ListRequest{
			Path: p, State: state, StateValue: prior,
			PlanValue: types.ListUnknown(elem), ConfigValue: types.ListNull(elem),
		}
		resp := &planmodifier.ListResponse{PlanValue: req.PlanValue}
		for _, m := range sa.PlanModifiers {
			m.PlanModifyList(ctx, req, resp)
		}
		return resp.PlanValue
	}
	t.Fatalf("%s is %T, which the test does not handle", p, a)
	return nil
}

// eachModifiedChild calls fn for every section child that carries
// useStateUnlessParentNew.
func eachModifiedChild(
	t *testing.T,
	fn func(sch schema.Schema, section, child string, a schema.Attribute),
) {
	t.Helper()
	var sr resource.SchemaResponse
	(&settingResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	count := 0
	for section, sa := range sr.Schema.Attributes {
		sec, ok := sa.(schema.SingleNestedAttribute)
		if !ok {
			continue
		}
		for child, a := range sec.Attributes {
			if !carriesParentModifier(a) {
				continue
			}
			count++
			fn(sr.Schema, section, child, a)
		}
	}
	if count != 87 {
		t.Errorf("found %d section children with useStateUnlessParentNew, want 87", count)
	}
}

func carriesParentModifier(a schema.Attribute) bool {
	var mods []any
	switch sa := a.(type) {
	case schema.BoolAttribute:
		for _, m := range sa.PlanModifiers {
			mods = append(mods, m)
		}
	case schema.Int64Attribute:
		for _, m := range sa.PlanModifiers {
			mods = append(mods, m)
		}
	case schema.StringAttribute:
		for _, m := range sa.PlanModifiers {
			mods = append(mods, m)
		}
	case schema.ListAttribute:
		for _, m := range sa.PlanModifiers {
			mods = append(mods, m)
		}
	case schema.ListNestedAttribute:
		for _, m := range sa.PlanModifiers {
			mods = append(mods, m)
		}
	}
	for _, m := range mods {
		if _, ok := m.(useStateUnlessParentNewModifier); ok {
			return true
		}
	}
	return false
}

// A section added to the configuration while state holds none must plan its
// computed children as unknown, so a value the controller sets on the write
// is accepted.
func TestSetting_NewSectionLeavesChildrenUnknown(t *testing.T) {
	eachModifiedChild(t, func(sch schema.Schema, section, child string, a schema.Attribute) {
		state := settingState(t, sch, "", "", nil)
		got := planChild(t, a, state, path.Root(section).AtName(child))
		if !got.IsUnknown() {
			t.Errorf("%s.%s planned %s for a new section, want unknown", section, child, got)
		}
	})
}

// A child the controller keeps null in an existing section must plan null, so
// an unrelated change does not plan it as known after apply.
func TestSetting_ExistingSectionKeepsANullChild(t *testing.T) {
	eachModifiedChild(t, func(sch schema.Schema, section, child string, a schema.Attribute) {
		state := settingState(t, sch, section, child, nil)
		got := planChild(t, a, state, path.Root(section).AtName(child))
		if got.IsUnknown() || !got.IsNull() {
			t.Errorf("%s.%s planned %s, want the prior null", section, child, got)
		}
	})
}

func TestSetting_ExistingSectionKeepsAKnownChild(t *testing.T) {
	var sr resource.SchemaResponse
	(&settingResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	syslog, ok := sr.Schema.Attributes["syslog"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("syslog is not a single nested attribute")
	}
	a := syslog.Attributes["ip"]
	state := settingState(t, sr.Schema, "syslog", "ip", types.StringValue("192.168.40.5"))
	got := planChild(t, a, state, path.Root("syslog").AtName("ip"))
	if !got.Equal(types.StringValue("192.168.40.5")) {
		t.Errorf("syslog.ip planned %s, want the prior value", got)
	}
}
