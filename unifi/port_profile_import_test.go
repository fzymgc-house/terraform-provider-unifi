package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// A profile the controller stores without dot1x_idle_timeout must import with no change, so the
// attribute has no schema default and an absent value reads back as null.
func Test_portProfileResource_dot1xIdleTimeoutHasNoDefault(t *testing.T) {
	r := &portProfileResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	attr, ok := resp.Schema.Attributes["dot1x_idle_timeout"].(schema.StringAttribute)
	if !ok {
		t.Fatalf(
			"dot1x_idle_timeout is %T, want schema.StringAttribute",
			resp.Schema.Attributes["dot1x_idle_timeout"],
		)
	}
	if attr.Default != nil {
		t.Errorf(
			"dot1x_idle_timeout has a schema default %v; an import of a profile without the key would plan a change",
			attr.Default,
		)
	}
	if !attr.Optional || !attr.Computed {
		t.Errorf(
			"dot1x_idle_timeout Optional=%v Computed=%v, want both true",
			attr.Optional,
			attr.Computed,
		)
	}
}

func Test_portProfileResource_portProfileToModel_absentDot1xIdleTimeoutIsNull(t *testing.T) {
	r := &portProfileResource{}
	model := &portProfileResourceModel{}
	diags := r.portProfileToModel(
		context.Background(),
		&unifi.PortProfile{ID: "p1", Name: "MobileInternet", OpMode: "switch"},
		model,
		"default",
	)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !model.Dot1XIdleTimeout.IsNull() {
		t.Errorf(
			"Dot1XIdleTimeout = %v, want null for a profile without the key",
			model.Dot1XIdleTimeout,
		)
	}
	seconds := int64(300)
	model = &portProfileResourceModel{}
	r.portProfileToModel(
		context.Background(),
		&unifi.PortProfile{
			ID:               "p2",
			Name:             "AllMainDefault",
			OpMode:           "switch",
			Dot1XIDleTimeout: &seconds,
		},
		model,
		"default",
	)
	if model.Dot1XIdleTimeout.ValueString() != "5m0s" {
		t.Errorf("Dot1XIdleTimeout = %q, want 5m0s", model.Dot1XIdleTimeout.ValueString())
	}
}
