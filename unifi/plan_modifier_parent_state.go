package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// useStateUnlessParentNew keeps an attribute's prior value, null included,
// while the object that holds it existed in the prior state. When that object
// is new, the value stays unknown, so the value the controller sets on the
// write is accepted.
//
// UseStateForUnknown copies a null prior value when the parent object is new,
// which fails the apply once the controller fills the attribute.
// UseNonNullStateForUnknown leaves every null prior value unknown, which plans
// known after apply on attributes the controller keeps null.
func useStateUnlessParentNew() useStateUnlessParentNewModifier {
	return useStateUnlessParentNewModifier{}
}

type useStateUnlessParentNewModifier struct{}

func (m useStateUnlessParentNewModifier) Description(context.Context) string {
	return "Keeps the prior value while the parent object existed in the prior state."
}

func (m useStateUnlessParentNewModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// usePrior reports whether the planned value takes the prior one. A prior
// value that is not null proves the parent existed. A null one needs the parent
// read from the state.
func (m useStateUnlessParentNewModifier) usePrior(
	ctx context.Context,
	state tfsdk.State,
	p path.Path,
	planUnknown, configUnknown, priorNull bool,
) bool {
	if state.Raw.IsNull() || !planUnknown || configUnknown {
		return false
	}
	if !priorNull {
		return true
	}
	if state.Schema == nil {
		return false
	}
	var parent types.Object
	if d := state.GetAttribute(ctx, p.ParentPath(), &parent); d.HasError() {
		return false
	}
	return !parent.IsNull() && !parent.IsUnknown()
}

func (m useStateUnlessParentNewModifier) PlanModifyBool(
	ctx context.Context,
	req planmodifier.BoolRequest,
	resp *planmodifier.BoolResponse,
) {
	if m.usePrior(
		ctx,
		req.State,
		req.Path,
		req.PlanValue.IsUnknown(),
		req.ConfigValue.IsUnknown(),
		req.StateValue.IsNull(),
	) {
		resp.PlanValue = req.StateValue
	}
}

func (m useStateUnlessParentNewModifier) PlanModifyInt64(
	ctx context.Context,
	req planmodifier.Int64Request,
	resp *planmodifier.Int64Response,
) {
	if m.usePrior(
		ctx,
		req.State,
		req.Path,
		req.PlanValue.IsUnknown(),
		req.ConfigValue.IsUnknown(),
		req.StateValue.IsNull(),
	) {
		resp.PlanValue = req.StateValue
	}
}

func (m useStateUnlessParentNewModifier) PlanModifyList(
	ctx context.Context,
	req planmodifier.ListRequest,
	resp *planmodifier.ListResponse,
) {
	if m.usePrior(
		ctx,
		req.State,
		req.Path,
		req.PlanValue.IsUnknown(),
		req.ConfigValue.IsUnknown(),
		req.StateValue.IsNull(),
	) {
		resp.PlanValue = req.StateValue
	}
}

func (m useStateUnlessParentNewModifier) PlanModifyString(
	ctx context.Context,
	req planmodifier.StringRequest,
	resp *planmodifier.StringResponse,
) {
	if m.usePrior(
		ctx,
		req.State,
		req.Path,
		req.PlanValue.IsUnknown(),
		req.ConfigValue.IsUnknown(),
		req.StateValue.IsNull(),
	) {
		resp.PlanValue = req.StateValue
	}
}
