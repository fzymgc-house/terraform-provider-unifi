package unifi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

var (
	_ resource.Resource                   = &devicePortsResource{}
	_ resource.ResourceWithConfigure      = &devicePortsResource{}
	_ resource.ResourceWithImportState    = &devicePortsResource{}
	_ resource.ResourceWithModifyPlan     = &devicePortsResource{}
	_ resource.ResourceWithValidateConfig = &devicePortsResource{}
)

func NewDevicePortsResource() resource.Resource {
	return &devicePortsResource{}
}

type devicePortsResource struct {
	client *Client
}

type devicePortsResourceModel struct {
	ID    types.String       `tfsdk:"id"`
	MAC   hwtypes.MACAddress `tfsdk:"mac"`
	Site  types.String       `tfsdk:"site"`
	Ports types.Map          `tfsdk:"ports"`
}

func (r *devicePortsResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_device_ports"
}

func devicePortAttributes() map[string]schema.Attribute {
	attrs := make(map[string]schema.Attribute, len(portFields))
	for _, f := range portFields {
		optional := !f.computedOnly
		switch f.kind {
		case portFieldBool:
			attrs[f.name] = schema.BoolAttribute{
				MarkdownDescription: f.description,
				Optional:            optional,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			}
		case portFieldInt:
			attrs[f.name] = schema.Int64Attribute{
				MarkdownDescription: f.description,
				Optional:            optional,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			}
		case portFieldStringSet:
			attrs[f.name] = schema.SetAttribute{
				MarkdownDescription: f.description,
				ElementType:         types.StringType,
				Optional:            optional,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			}
		case portFieldIntSet:
			attrs[f.name] = schema.SetAttribute{
				MarkdownDescription: f.description,
				ElementType:         types.Int64Type,
				Optional:            optional,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			}
		default:
			var validators []validator.String
			if len(f.oneOf) > 0 {
				validators = append(validators, stringvalidator.OneOf(f.oneOf...))
			}
			attrs[f.name] = schema.StringAttribute{
				MarkdownDescription: f.description,
				Optional:            optional,
				Computed:            true,
				Validators:          validators,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			}
		}
	}
	return attrs
}

func (r *devicePortsResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Declares the complete port configuration of a UniFi device, typically a switch. " +
			"Every port listed in `ports` is defined as configured; every port not listed runs the controller's " +
			"default configuration. This is the one way to manage ports: do not also declare `port_override` " +
			"blocks on the same device's `unifi_device`, and declare at most one `unifi_device_ports` per device.\n\n" +
			"A port attribute left out of the configuration is read from the controller and left as it is; " +
			"removing an attribute from the configuration does not reset it. Keys the controller holds that this " +
			"resource does not model are always written back unchanged.\n\n" +
			"Destroying the resource resets every port on the device to the default configuration. Use a " +
			"`removed` block with `destroy = false` to stop managing a device without touching its ports.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The controller ID of the device.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"mac": schema.StringAttribute{
				MarkdownDescription: "The MAC address of the device.",
				Required:            true,
				CustomType:          hwtypes.MACAddressType{},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site the device belongs to. Defaults to the provider's site.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ports": schema.MapNestedAttribute{
				MarkdownDescription: "Port definitions keyed by port index (`\"1\"`, `\"2\"`, ...). A link aggregation " +
					"group is declared on its lead port only.",
				Required: true,
				Validators: []validator.Map{
					mapvalidator.KeysAre(
						stringvalidator.RegexMatches(
							portIndexKey,
							"must be a port index such as \"12\"",
						),
					),
				},
				NestedObject: schema.NestedAttributeObject{Attributes: devicePortAttributes()},
			},
		},
	}
}

func (r *devicePortsResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}
	r.client = client
}

// ValidateConfig rejects a member port of a link aggregation group declared as
// a port of its own: the controller stores the group on its lead port only.
func (r *devicePortsResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var ports types.Map
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("ports"), &ports)...)
	if resp.Diagnostics.HasError() || ports.IsNull() || ports.IsUnknown() {
		return
	}
	for _, lead := range devicePortsKeysSorted(ports.Elements()) {
		obj, ok := ports.Elements()[lead].(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		members, ok := obj.Attributes()["aggregate_members"].(types.Set)
		if !ok || members.IsNull() || members.IsUnknown() {
			continue
		}
		var idxs []int64
		resp.Diagnostics.Append(members.ElementsAs(ctx, &idxs, false)...)
		for _, idx := range idxs {
			key := strconv.FormatInt(idx, 10)
			if key == lead {
				continue
			}
			if _, declared := ports.Elements()[key]; declared {
				resp.Diagnostics.AddAttributeError(
					path.Root("ports").AtMapKey(key),
					"Link aggregation member declared as a port",
					fmt.Sprintf(
						"Port %s is a member of the group led by port %s; declare the group on port %s only.",
						key,
						lead,
						lead,
					),
				)
			}
		}
	}
}

// ModifyPlan leaves undeclared attributes of a changed port unknown. Every port
// attribute keeps its prior value by default (UseStateForUnknown), which keeps
// plans for unchanged ports empty. A write can change keys the configuration
// does not declare on the port it touches, though: the controller assigns
// lag_idx to a new aggregation lead and adds voice_networkconf_id when a port
// stops tagging VLANs. Pinning those to their prior values would fail the apply
// with an inconsistent result.
func (r *devicePortsResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if req.Plan.Raw.IsNull() || req.State.Raw.IsNull() {
		return
	}
	var plan, state, config devicePortsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || plan.Ports.IsNull() || plan.Ports.IsUnknown() ||
		state.Ports.IsNull() {
		return
	}

	attrTypes := devicePortAttrTypes()
	ports := make(map[string]attr.Value, len(plan.Ports.Elements()))
	modified := false
	for key, elem := range plan.Ports.Elements() {
		planObj, ok := elem.(types.Object)
		if !ok || planObj.IsNull() || planObj.IsUnknown() {
			ports[key] = elem
			continue
		}
		var configAttrs, stateAttrs map[string]attr.Value
		if c, ok := config.Ports.Elements()[key].(types.Object); ok && !c.IsNull() &&
			!c.IsUnknown() {
			configAttrs = c.Attributes()
		}
		if s, ok := state.Ports.Elements()[key].(types.Object); ok && !s.IsNull() {
			stateAttrs = s.Attributes()
		}
		declared := func(name string) bool {
			v, ok := configAttrs[name]
			return ok && !v.IsNull()
		}

		changed := stateAttrs == nil
		for _, f := range portFields {
			if !changed && !f.computedOnly && declared(f.name) &&
				!planObj.Attributes()[f.name].Equal(stateAttrs[f.name]) {
				changed = true
			}
		}
		if !changed {
			ports[key] = planObj
			continue
		}

		values := make(map[string]attr.Value, len(portFields))
		for _, f := range portFields {
			if !f.computedOnly && declared(f.name) {
				values[f.name] = planObj.Attributes()[f.name]
			} else {
				values[f.name] = unknownPortValue(f.kind)
			}
		}
		obj, d := types.ObjectValue(attrTypes, values)
		resp.Diagnostics.Append(d...)
		ports[key] = obj
		modified = true
	}
	if !modified || resp.Diagnostics.HasError() {
		return
	}
	m, d := types.MapValue(types.ObjectType{AttrTypes: attrTypes}, ports)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ports"), m)...)
}

func (r *devicePortsResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan, config devicePortsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	site := r.siteOf(plan.Site)

	device, err := r.getDevice(ctx, site, plan.MAC.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read device", err.Error())
		return
	}
	if len(device.PortOverrides) > 0 {
		resp.Diagnostics.AddError(
			"Device already has port definitions",
			fmt.Sprintf(
				"Device %s has %d port definitions on the controller. Import it so the configuration starts "+
					"from them: terraform import <address> %s:%s",
				device.MAC,
				len(device.PortOverrides),
				site,
				device.MAC,
			),
		)
		return
	}

	resp.Diagnostics.Append(r.write(ctx, site, device, plan.Ports, config.Ports)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.refreshAfterWrite(ctx, site, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *devicePortsResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state devicePortsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, diags := r.refresh(ctx, r.siteOf(state.Site), &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *devicePortsResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, config devicePortsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	site := r.siteOf(plan.Site)

	device, err := r.getDevice(ctx, site, plan.MAC.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read device", err.Error())
		return
	}
	resp.Diagnostics.Append(r.write(ctx, site, device, plan.Ports, config.Ports)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.refreshAfterWrite(ctx, site, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete resets every port on the device to the default configuration.
func (r *devicePortsResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state devicePortsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	site := r.siteOf(state.Site)

	device, err := r.getDevice(ctx, site, state.MAC.ValueString())
	if errors.Is(err, errDeviceNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read device", err.Error())
		return
	}
	if err := r.putPortOverrides(ctx, site, device.ID, []portOverrideEntry{}); err != nil {
		resp.Diagnostics.AddError("Unable to reset device ports", err.Error())
	}
}

// ImportState accepts "<mac>" or "<site>:<mac>". A MAC address has five
// colons, so a sixth marks a site prefix.
func (r *devicePortsResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	site, mac := r.client.Site, req.ID
	if parts := strings.SplitN(req.ID, ":", 2); strings.Count(req.ID, ":") == 6 {
		site, mac = parts[0], parts[1]
	}
	mac = cleanMAC(mac)
	resp.Diagnostics.Append(
		resp.State.SetAttribute(ctx, path.Root("mac"), hwtypes.NewMACAddressValue(mac))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(
		ctx,
		path.Root("ports"),
		types.MapNull(types.ObjectType{AttrTypes: devicePortAttrTypes()}),
	)...)
}

var errDeviceNotFound = errors.New("device not found")

func (r *devicePortsResource) siteOf(site types.String) string {
	if site.IsNull() || site.IsUnknown() || site.ValueString() == "" {
		return r.client.Site
	}
	return site.ValueString()
}

// getDevice reads the device record as raw JSON, so that port_overrides keeps
// every key the controller holds.
func (r *devicePortsResource) getDevice(ctx context.Context, site, mac string) (*rawDevice, error) {
	var body struct {
		Data []rawDevice `json:"data"`
	}
	err := r.client.Do(
		ctx,
		http.MethodGet,
		fmt.Sprintf("api/s/%s/stat/device/%s", site, cleanMAC(mac)),
		nil,
		&body,
	)
	var notFound *unifi.NotFoundError
	if errors.As(err, &notFound) {
		return nil, errDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	for i := range body.Data {
		if cleanMAC(body.Data[i].MAC) == cleanMAC(mac) {
			return &body.Data[i], nil
		}
	}
	return nil, errDeviceNotFound
}

// putPortOverrides replaces the device's port_overrides array and sends no
// other device field.
func (r *devicePortsResource) putPortOverrides(
	ctx context.Context,
	site, id string,
	entries []portOverrideEntry,
) error {
	if entries == nil {
		entries = []portOverrideEntry{}
	}
	body := map[string]any{"port_overrides": entries}
	return r.client.Do(
		ctx,
		http.MethodPut,
		fmt.Sprintf("api/s/%s/rest/device/%s", site, id),
		body,
		nil,
	)
}

func (r *devicePortsResource) write(
	ctx context.Context,
	site string,
	device *rawDevice,
	plan, config types.Map,
) diag.Diagnostics {
	var diags diag.Diagnostics
	declared, d := declaredPorts(ctx, plan, config)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	entries, err := mergePortOverrides(device.PortOverrides, declared)
	if err != nil {
		diags.AddError("Unable to merge port definitions", err.Error())
		return diags
	}
	if err := r.putPortOverrides(ctx, site, device.ID, entries); err != nil {
		diags.AddError("Unable to write port definitions", err.Error())
	}
	return diags
}

// refresh reads the device and sets the model's device ID, site, and one ports
// element per port_overrides entry. found is false when the device no longer
// exists on the controller.
func (r *devicePortsResource) refresh(
	ctx context.Context,
	site string,
	m *devicePortsResourceModel,
) (found bool, diags diag.Diagnostics) {
	device, err := r.getDevice(ctx, site, m.MAC.ValueString())
	if errors.Is(err, errDeviceNotFound) {
		return false, diags
	}
	if err != nil {
		diags.AddError("Unable to read device", err.Error())
		return false, diags
	}
	ports, d := portOverridesToState(device.PortOverrides)
	diags.Append(d...)
	if diags.HasError() {
		return true, diags
	}
	m.ID = types.StringValue(device.ID)
	m.Site = types.StringValue(site)
	m.Ports = ports
	return true, diags
}

func (r *devicePortsResource) refreshAfterWrite(
	ctx context.Context,
	site string,
	m *devicePortsResourceModel,
) diag.Diagnostics {
	found, diags := r.refresh(ctx, site, m)
	if !found && !diags.HasError() {
		diags.AddError(
			"Unable to read device",
			fmt.Sprintf("device %s disappeared after the write", m.MAC.ValueString()),
		)
	}
	return diags
}

var portIndexKey = regexp.MustCompile(`^[1-9][0-9]*$`)

// devicePortsKeysSorted returns the ports map keys in numeric order, for stable
// diagnostics.
func devicePortsKeysSorted(ports map[string]attr.Value) []string {
	keys := make([]string, 0, len(ports))
	for k := range ports {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ai, _ := strconv.Atoi(a)
		bi, _ := strconv.Atoi(b)
		return ai - bi
	})
	return keys
}
