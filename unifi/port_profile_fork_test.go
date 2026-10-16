package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
