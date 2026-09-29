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

// existingState returns the state of a network that exists, so plan modifiers
// run as they do on an update.
func existingState(t *testing.T) (schema.Schema, tfsdk.State) {
	t.Helper()
	ctx := context.Background()
	var sch resource.SchemaResponse
	(&networkResource{}).Schema(ctx, resource.SchemaRequest{}, &sch)
	s := tfsdk.State{
		Schema: sch.Schema,
		Raw:    tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil),
	}
	if d := s.SetAttribute(ctx, path.Root("name"), types.StringValue("Guest")); d.HasError() {
		t.Fatal(d)
	}
	return sch.Schema, s
}

func dhcpServerAttr(t *testing.T, sch schema.Schema, name string) schema.Attribute {
	t.Helper()
	server, ok := sch.Attributes["dhcp_server"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("dhcp_server is not a single nested attribute")
	}
	a, ok := server.Attributes[name]
	if !ok {
		t.Fatalf("dhcp_server has no %q attribute", name)
	}
	return a
}

func planString(
	t *testing.T,
	a schema.Attribute,
	state tfsdk.State,
	prior types.String,
) types.String {
	t.Helper()
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("attribute is %T, want a string attribute", a)
	}
	req := planmodifier.StringRequest{
		State:       state,
		StateValue:  prior,
		PlanValue:   types.StringUnknown(),
		ConfigValue: types.StringNull(),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	for _, m := range sa.PlanModifiers {
		m.PlanModifyString(context.Background(), req, resp)
	}
	return resp.PlanValue
}

// A dhcp_server block added to a network that had none must leave start and
// stop unknown. The controller derives them from the subnet on the write, and
// a null plan fails the apply with an inconsistent result.
func TestNetworkDhcpServer_NewBlockLeavesTheRangeUnknown(t *testing.T) {
	sch, state := existingState(t)
	for _, name := range []string{"start", "stop"} {
		got := planString(t, dhcpServerAttr(t, sch, name), state, types.StringNull())
		if !got.IsUnknown() {
			t.Errorf("dhcp_server.%s planned %s, want unknown", name, got)
		}
	}
}

func TestNetworkDhcpServer_KnownRangeKeepsItsValue(t *testing.T) {
	sch, state := existingState(t)
	got := planString(t, dhcpServerAttr(t, sch, "start"), state, types.StringValue("192.168.79.6"))
	if !got.Equal(types.StringValue("192.168.79.6")) {
		t.Errorf("dhcp_server.start planned %s, want the prior value", got)
	}
}

func TestNetworkDhcpServer_NewBlockLeavesBootUnknown(t *testing.T) {
	sch, state := existingState(t)
	boot, ok := dhcpServerAttr(t, sch, "boot").(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("dhcp_server.boot is not a single nested attribute")
	}
	attrTypes := map[string]attr.Type{
		"enabled":  types.BoolType,
		"server":   types.StringType,
		"filename": types.StringType,
	}
	req := planmodifier.ObjectRequest{
		State:       state,
		StateValue:  types.ObjectNull(attrTypes),
		PlanValue:   types.ObjectUnknown(attrTypes),
		ConfigValue: types.ObjectNull(attrTypes),
	}
	resp := &planmodifier.ObjectResponse{PlanValue: req.PlanValue}
	for _, m := range boot.PlanModifiers {
		m.PlanModifyObject(context.Background(), req, resp)
	}
	if !resp.PlanValue.IsUnknown() {
		t.Errorf("dhcp_server.boot planned %s, want unknown", resp.PlanValue)
	}
}
