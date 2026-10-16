package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// The controller replaces the whole network on update. preserveUnmanagedKeys carries the keys
// the resource does not model, or models only when configured, from the network as the
// controller holds it, and leaves a key the plan set alone.
func Test_preserveUnmanagedKeys(t *testing.T) {
	zone := "zone-live"
	planZone := "zone-plan"
	v6 := true
	slaac := true
	pref := "manual"
	current := &unifi.Network{
		ExternalID:            "ext-1",
		IPV6Enabled:           &v6,
		FirewallZoneID:        &zone,
		IPV6Aliases:           []string{"fd00:601::1/64"},
		IPV6SettingPreference: &pref,
		DHCPDV6AllowSlaac:     &slaac,
	}

	unset := &unifi.Network{}
	preserveUnmanagedKeys(unset, current)
	if unset.ExternalID != "ext-1" || unset.IPV6Enabled == nil || !*unset.IPV6Enabled {
		t.Errorf("external_id / ipv6_enabled not carried: %+v", unset)
	}
	if unset.FirewallZoneID == nil || *unset.FirewallZoneID != "zone-live" {
		t.Errorf("FirewallZoneID = %v, want the live zone", unset.FirewallZoneID)
	}
	if len(unset.IPV6Aliases) != 1 || unset.IPV6SettingPreference == nil || unset.DHCPDV6AllowSlaac == nil {
		t.Errorf("an unset key lost its live value: %+v", unset)
	}

	stated := &unifi.Network{FirewallZoneID: &planZone, IPV6Aliases: []string{}}
	preserveUnmanagedKeys(stated, current)
	if *stated.FirewallZoneID != "zone-plan" {
		t.Errorf("FirewallZoneID = %q, want the planned zone kept", *stated.FirewallZoneID)
	}
	if len(stated.IPV6Aliases) != 0 {
		t.Errorf("IPV6Aliases = %v, want the planned empty list kept", stated.IPV6Aliases)
	}
}

// A declared ipv6.aliases list reaches the wire, a configured empty list clears it, and a
// null list leaves the field nil for Update to carry the live value.
func Test_networkResource_ipv6Aliases_toNetwork(t *testing.T) {
	ctx := context.Background()
	r := &networkResource{}

	withAliases := func(aliases types.List, pref types.String) *networkResourceModel {
		m := &networkResourceModel{
			Name:    types.StringValue("n"),
			Purpose: types.StringValue("corporate"),
			IPv6: types.ObjectValueMust(networkIPv6AttrTypes(), map[string]attr.Value{
				"interface_type":            types.StringValue("pd"),
				"client_address_assignment": types.StringNull(),
				"static_subnet":             types.StringNull(),
				"aliases":                   aliases,
				"setting_preference":        pref,
				"ra":                        types.ObjectNull(networkIPv6RAAttrTypes()),
				"pd":                        types.ObjectNull(networkIPv6PDAttrTypes()),
			}),
		}
		return m
	}

	stated, diags := r.modelToNetwork(ctx, withAliases(
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("fd00:601::1/64")}),
		types.StringValue("manual"),
	))
	if diags.HasError() {
		t.Fatalf("modelToNetwork: %v", diags)
	}
	if len(stated.IPV6Aliases) != 1 || stated.IPV6Aliases[0] != "fd00:601::1/64" {
		t.Errorf("IPV6Aliases = %v, want the declared alias", stated.IPV6Aliases)
	}
	if stated.IPV6SettingPreference == nil || *stated.IPV6SettingPreference != "manual" {
		t.Errorf("IPV6SettingPreference = %v, want manual", stated.IPV6SettingPreference)
	}

	cleared, _ := r.modelToNetwork(ctx, withAliases(
		types.ListValueMust(types.StringType, []attr.Value{}),
		types.StringNull(),
	))
	if cleared.IPV6Aliases == nil || len(cleared.IPV6Aliases) != 0 {
		t.Errorf("IPV6Aliases = %#v, want an empty non-nil list for a configured []", cleared.IPV6Aliases)
	}
	if cleared.IPV6SettingPreference != nil {
		t.Errorf("IPV6SettingPreference = %v, want nil for an unset attribute", *cleared.IPV6SettingPreference)
	}

	unset, _ := r.modelToNetwork(ctx, withAliases(types.ListNull(types.StringType), types.StringNull()))
	if unset.IPV6Aliases != nil {
		t.Errorf("IPV6Aliases = %#v, want nil for a null list", unset.IPV6Aliases)
	}
}

// ipv6.setting_preference and ipv6.aliases read back only as the configuration manages them:
// setting_preference stays null unless the prior model held one, and an empty alias list
// reads as a known empty list only when the prior model held a list.
func Test_ipv6ManagedLeaves(t *testing.T) {
	ctx := context.Background()
	pref := "manual"
	network := &unifi.Network{IPV6SettingPreference: &pref}

	api, diags := networkIPv6FromAPI(ctx, network)
	if diags.HasError() {
		t.Fatalf("networkIPv6FromAPI: %v", diags)
	}
	var fromAPI networkIPv6Model
	if d := api.As(ctx, &fromAPI, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("decode: %v", d)
	}
	if !fromAPI.SettingPreference.IsNull() || !fromAPI.Aliases.IsNull() {
		t.Errorf("the API shape must start null: %+v", fromAPI)
	}

	previous := types.ObjectValueMust(networkIPv6AttrTypes(), map[string]attr.Value{
		"interface_type":            types.StringValue("pd"),
		"client_address_assignment": types.StringNull(),
		"static_subnet":             types.StringNull(),
		"aliases":                   types.ListValueMust(types.StringType, []attr.Value{}),
		"setting_preference":        types.StringValue("manual"),
		"ra":                        types.ObjectNull(networkIPv6RAAttrTypes()),
		"pd":                        types.ObjectNull(networkIPv6PDAttrTypes()),
	})
	managed, diags := ipv6ManagedLeaves(ctx, api, previous, network)
	if diags.HasError() {
		t.Fatalf("ipv6ManagedLeaves: %v", diags)
	}
	var out networkIPv6Model
	if d := managed.As(ctx, &out, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("decode: %v", d)
	}
	if out.SettingPreference.ValueString() != "manual" {
		t.Errorf("SettingPreference = %v, want manual once the configuration states it", out.SettingPreference)
	}
	if out.Aliases.IsNull() || len(out.Aliases.Elements()) != 0 {
		t.Errorf("Aliases = %v, want a known empty list for a configured []", out.Aliases)
	}

	untouched, _ := ipv6ManagedLeaves(ctx, api, types.ObjectNull(networkIPv6AttrTypes()), network)
	if !untouched.Equal(api) {
		t.Errorf("a null prior model must leave the API shape alone")
	}
}
