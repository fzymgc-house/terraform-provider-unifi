package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

const (
	aclRuleTypeMAC   = "MAC"
	aclSideClientMAC = "CLIENT_MAC"
)

var (
	_ resource.Resource                = &aclRuleResource{}
	_ resource.ResourceWithImportState = &aclRuleResource{}
	_ resource.ResourceWithIdentity    = &aclRuleResource{}
)

func NewACLRuleResource() resource.Resource {
	return &aclRuleResource{}
}

type aclRuleResource struct {
	client *Client
}

type aclRuleResourceModel struct {
	ID              types.String   `tfsdk:"id"`
	Site            types.String   `tfsdk:"site"`
	Name            types.String   `tfsdk:"name"`
	Description     types.String   `tfsdk:"description"`
	Enabled         types.Bool     `tfsdk:"enabled"`
	Action          types.String   `tfsdk:"action"`
	Index           types.Int64    `tfsdk:"acl_index"`
	NetworkID       types.String   `tfsdk:"network_id"`
	SwitchMACs      types.Set      `tfsdk:"switch_macs"`
	SourceMACs      types.Set      `tfsdk:"source_macs"`
	DestinationMACs types.Set      `tfsdk:"destination_macs"`
	Timeouts        timeouts.Value `tfsdk:"timeouts"`
}

type aclRuleIdentityModel struct {
	ID   types.String `tfsdk:"id"`
	Site types.String `tfsdk:"site"`
}

func (r *aclRuleResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_acl_rule"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *aclRuleResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
			"site": identityschema.StringAttribute{
				OptionalForImport: true,
			},
		},
	}
}

func (r *aclRuleResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	macSet := func(description string, validators ...validator.Set) schema.SetAttribute {
		return schema.SetAttribute{
			Description: description,
			Required:    true,
			ElementType: hwtypes.MACAddressType{},
			Validators:  validators,
		}
	}

	resp.Schema = schema.Schema{
		Description: "`unifi_acl_rule` manages a MAC ACL rule of the switches (Settings, Policy Table, MAC ACL). " +
			"A switch evaluates the rules of a network in `acl_index` order where a frame enters it. " +
			"The resource manages MAC rules only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the ACL rule.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"site": schema.StringAttribute{
				Description: "The name of the site of the ACL rule.",
				Computed:    true,
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the ACL rule.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				Description: "The description of the ACL rule.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the switches enforce the rule. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"action": schema.StringAttribute{
				Description: "`ALLOW` or `BLOCK`.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALLOW", "BLOCK"),
				},
			},
			"acl_index": schema.Int64Attribute{
				Description: "The position of the rule. A switch evaluates a lower index first.",
				Required:    true,
			},
			"network_id": schema.StringAttribute{
				Description: "The ID of the network that the rule applies to.",
				Required:    true,
			},
			"switch_macs": macSet(
				"The MAC addresses of the switches that enforce the rule. An empty set puts the rule on every switch.",
			),
			"source_macs": macSet(
				"The source MAC addresses that the rule matches. Must not be empty: the controller reads an "+
					"empty source as every device, so a `BLOCK` rule would cut off the whole network.",
				setvalidator.SizeAtLeast(1),
			),
			"destination_macs": macSet(
				"The destination MAC addresses that the rule matches. An empty set matches every device.",
			),
			"timeouts": timeouts.Attributes(
				ctx,
				timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
			),
		},
	}
}

func (r *aclRuleResource) Configure(
	ctx context.Context,
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

func (r *aclRuleResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan aclRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	site := plan.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	rule, d := planOntoACLRule(ctx, &plan, nil)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateAclRule(ctx, site, rule)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating ACL Rule",
			"Could not create the ACL rule: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(aclRuleToModel(ctx, created, &plan, site)...)
	resp.Diagnostics.Append(
		resp.Identity.Set(ctx, aclRuleIdentityModel{ID: plan.ID, Site: plan.Site})...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *aclRuleResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state aclRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, d := state.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	if state.ID.IsNull() || state.ID.IsUnknown() {
		if req.Identity != nil && !req.Identity.Raw.IsNull() {
			var identity aclRuleIdentityModel
			resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
			if resp.Diagnostics.HasError() {
				return
			}
			state.ID = identity.ID
			if (state.Site.IsNull() || state.Site.IsUnknown()) &&
				identity.Site.ValueString() != "" {
				state.Site = identity.Site
			}
		}
	}

	id := state.ID.ValueString()
	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	rule, err := r.client.GetAclRule(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading ACL Rule",
			fmt.Sprintf("Could not read ACL rule %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(aclRuleToModel(ctx, rule, &state, site)...)

	// Terraform rejects any change to a stored identity, so a stored one passes through.
	if req.Identity == nil || req.Identity.Raw.IsNull() {
		resp.Diagnostics.Append(
			resp.Identity.Set(ctx, aclRuleIdentityModel{ID: state.ID, Site: state.Site})...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *aclRuleResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, state aclRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, d := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	id := state.ID.ValueString()
	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// The UI sends the whole stored rule on an update, so the plan goes over the stored rule
	// and every key that the schema does not declare keeps its stored value.
	stored, err := r.client.GetAclRule(ctx, site, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading ACL Rule",
			fmt.Sprintf("Could not read ACL rule %s before the update: %s", id, err),
		)
		return
	}
	rule, d := planOntoACLRule(ctx, &plan, stored)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	rule.ID = id

	updated, err := r.client.UpdateAclRule(ctx, site, rule)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating ACL Rule",
			fmt.Sprintf("Could not update ACL rule %s: %s", id, err),
		)
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(aclRuleToModel(ctx, updated, &plan, site)...)

	if req.Identity == nil || req.Identity.Raw.IsNull() {
		resp.Diagnostics.Append(
			resp.Identity.Set(ctx, aclRuleIdentityModel{ID: plan.ID, Site: plan.Site})...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *aclRuleResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state aclRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	id := state.ID.ValueString()
	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	if err := r.client.DeleteAclRule(ctx, site, id); err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting ACL Rule",
			fmt.Sprintf("Could not delete ACL rule %s: %s", id, err),
		)
	}
}

func (r *aclRuleResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	if req.ID == "" {
		var identity aclRuleIdentityModel
		resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), identity.ID)...)
		if identity.Site.ValueString() != "" {
			resp.Diagnostics.Append(
				resp.State.SetAttribute(ctx, path.Root("site"), identity.Site)...)
		}
		return
	}

	// Import by "id" or "site:id".
	idParts, diags := util.ParseImportID(req.ID, 1, 2)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if site := idParts["site"]; site != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
		resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("site"), site)...)
	}
	if id := idParts["id"]; id != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), id)...)
	}
}

// planOntoACLRule lays the model over base, the rule as the controller stores it, and returns
// the rule to send. A nil base starts from an empty rule. Each MAC list is sent as a list of
// devices, which the UI sends without a MAC mask, so a write clears a stored mask.
func planOntoACLRule(
	ctx context.Context,
	model *aclRuleResourceModel,
	base *unifi.AclRule,
) (*unifi.AclRule, diag.Diagnostics) {
	var diags diag.Diagnostics

	switches := macsFromSet(ctx, model.SwitchMACs, &diags)
	sources := macsFromSet(ctx, model.SourceMACs, &diags)
	destinations := macsFromSet(ctx, model.DestinationMACs, &diags)
	if diags.HasError() {
		return nil, diags
	}
	if len(sources) == 0 {
		diags.AddAttributeError(
			path.Root("source_macs"),
			"Empty Source",
			"The controller reads an empty source as every device. Name at least one source MAC address.",
		)
		return nil, diags
	}

	rule := &unifi.AclRule{}
	if base != nil {
		stored := *base
		rule = &stored
	}
	index := model.Index.ValueInt64()
	rule.Type = aclRuleTypeMAC
	rule.Name = model.Name.ValueString()
	if !model.Description.IsNull() && !model.Description.IsUnknown() {
		rule.Description = model.Description.ValueString()
	}
	rule.Enabled = model.Enabled.ValueBool()
	rule.Action = model.Action.ValueString()
	rule.AclIndex = &index
	rule.MACAclNetworkID = model.NetworkID.ValueString()
	rule.SpecificEnforcers = switches

	source := unifi.AclRuleTrafficSource{}
	if rule.TrafficSource != nil {
		source = *rule.TrafficSource
	}
	source.Type = aclSideClientMAC
	source.SpecificMACAddresses = sources
	source.MACMask = ""
	rule.TrafficSource = &source

	destination := unifi.AclRuleTrafficDestination{}
	if rule.TrafficDestination != nil {
		destination = *rule.TrafficDestination
	}
	destination.Type = aclSideClientMAC
	destination.SpecificMACAddresses = destinations
	destination.MACMask = ""
	rule.TrafficDestination = &destination

	return rule, diags
}

// macsFromSet returns the MACs of a set in the stored form, lowercase and colon separated. The
// result is never nil, because an empty list is sent as [] and has a meaning of its own.
func macsFromSet(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	macs := []string{}
	if set.IsNull() || set.IsUnknown() {
		return macs
	}
	var values []string
	diags.Append(set.ElementsAs(ctx, &values, false)...)
	for _, mac := range values {
		macs = append(macs, cleanMAC(mac))
	}
	return macs
}

func aclRuleToModel(
	ctx context.Context,
	api *unifi.AclRule,
	model *aclRuleResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	if api.Type != aclRuleTypeMAC {
		diags.AddError(
			"Unsupported ACL Rule Type",
			fmt.Sprintf(
				"ACL rule %s has type %q. unifi_acl_rule manages MAC rules only.",
				api.ID,
				api.Type,
			),
		)
		return diags
	}

	if api.ID != "" {
		model.ID = types.StringValue(api.ID)
	}
	model.Site = types.StringValue(site)
	model.Name = types.StringValue(api.Name)
	model.Description = types.StringValue(api.Description)
	model.Enabled = types.BoolValue(api.Enabled)
	model.Action = types.StringValue(api.Action)
	if api.AclIndex != nil {
		model.Index = types.Int64Value(*api.AclIndex)
	} else {
		model.Index = types.Int64Null()
	}
	model.NetworkID = types.StringValue(api.MACAclNetworkID)

	var sources, destinations []string
	if api.TrafficSource != nil {
		sources = api.TrafficSource.SpecificMACAddresses
	}
	if api.TrafficDestination != nil {
		destinations = api.TrafficDestination.SpecificMACAddresses
	}
	model.SwitchMACs = macSetValue(ctx, api.SpecificEnforcers, &diags)
	model.SourceMACs = macSetValue(ctx, sources, &diags)
	model.DestinationMACs = macSetValue(ctx, destinations, &diags)

	return diags
}

// macSetValue maps a nil or empty list to an empty set, so a configured empty set reads back
// without a diff.
func macSetValue(ctx context.Context, macs []string, diags *diag.Diagnostics) types.Set {
	if macs == nil {
		macs = []string{}
	}
	set, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{}, macs)
	diags.Append(d...)
	return set
}
