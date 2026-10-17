package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// A profile the controller stores without dot1x_idle_timeout must import with no change, so
// neither the dot1x group nor its idle_timeout leaf carries a schema default: both are read
// from the controller when the configuration leaves them out.
func Test_portProfileResource_dot1xIdleTimeoutHasNoDefault(t *testing.T) {
	r := &portProfileResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	dot1x, ok := resp.Schema.Attributes["dot1x"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("dot1x is %T, want schema.SingleNestedAttribute", resp.Schema.Attributes["dot1x"])
	}
	if dot1x.Default != nil || !dot1x.IsComputed() || !dot1x.IsOptional() {
		t.Errorf("dot1x must be optional and computed with no default: %+v", dot1x)
	}
	idle, ok := dot1x.Attributes["idle_timeout"].(schema.StringAttribute)
	if !ok {
		t.Fatalf(
			"dot1x.idle_timeout is %T, want schema.StringAttribute",
			dot1x.Attributes["idle_timeout"],
		)
	}
	if idle.Default != nil {
		t.Errorf(
			"dot1x.idle_timeout has a schema default %v; an import of a profile without the key would plan a change",
			idle.Default,
		)
	}
	if !idle.Optional || !idle.Computed {
		t.Errorf(
			"dot1x.idle_timeout Optional=%v Computed=%v, want both true",
			idle.Optional,
			idle.Computed,
		)
	}
}

// An absent idle timeout reads as null, and a stored one reads as its duration.
func Test_portProfileResource_portProfileToModel_absentDot1xIdleTimeoutIsNull(t *testing.T) {
	ctx := context.Background()
	r := &portProfileResource{}
	read := func(p *unifi.PortProfile) portDot1xModel {
		model := &portProfileResourceModel{}
		if diags := r.portProfileToModel(ctx, p, model, "default"); diags.HasError() {
			t.Fatal(diags)
		}
		var dot1x portDot1xModel
		if diags := model.Dot1X.As(ctx, &dot1x, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatal(diags)
		}
		return dot1x
	}

	absent := read(&unifi.PortProfile{ID: "p1", Name: "MobileInternet", OpMode: "switch"})
	if !absent.IdleTimeout.IsNull() {
		t.Errorf("IdleTimeout = %v, want null for a profile without the key", absent.IdleTimeout)
	}

	seconds := int64(300)
	stored := read(&unifi.PortProfile{
		ID:               "p2",
		Name:             "AllMainDefault",
		OpMode:           "switch",
		Dot1XIDleTimeout: &seconds,
	})
	if stored.IdleTimeout.ValueString() != "5m0s" {
		t.Errorf("IdleTimeout = %q, want 5m0s", stored.IdleTimeout.ValueString())
	}
}

// The controller keeps forward = "customize" on a profile with tagged_vlan_mgmt = "custom" whose
// native network has no VLAN id and a tagged or an excluded network list, so the plan-time
// refusal for an untagged native network (#496)
// applies only while the controller manages the tagged VLANs itself.
func TestAccPortProfileFork_customizeOnUntaggedNativeWithCustomTaggedVLANs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "unifi_network" "default" {
	name = "Default"
}

# One tagged network stays allowed and one is excluded. A custom profile that
# excludes every tagged network collapses to block_all on the controller.
resource "unifi_network" "kept" {
	name    = "tfacc-customize-kept"
	purpose = "corporate"
	subnet  = "10.213.0.1/24"
	vlan    = 213
}

resource "unifi_network" "excluded" {
	name    = "tfacc-customize-excluded"
	purpose = "corporate"
	subnet  = "10.212.0.1/24"
	vlan    = 212
}

resource "unifi_port_profile" "test" {
	name                     = "tfacc-customize-untagged-native"
	native_networkconf_id    = data.unifi_network.default.id
	tagged_vlan_mgmt         = "custom"
	excluded_networkconf_ids = [unifi_network.excluded.id]
	forward                  = "customize"

	depends_on = [unifi_network.kept]
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_port_profile.test", "forward", "customize"),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.test", "tagged_vlan_mgmt", "custom"),
				),
			},
		},
	})
}
