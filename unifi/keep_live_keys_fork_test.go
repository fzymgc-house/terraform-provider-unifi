package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// The controller replaces the whole BGP configuration on every write. planOntoBGP lays the plan
// over the configuration as the controller holds it, so a key the resource never sets keeps its
// live value, and error, which the controller sets, never goes out.
func Test_bgpResource_planOntoBGP_keepsLiveFields(t *testing.T) {
	ctx := context.Background()
	r := &bgpResource{}

	liveError := true
	live := &unifi.BGPConfig{
		ID:                      "6ac2ccc4535c0e4cd8a8e1a2",
		Config:                  "router bgp 65000",
		Description:             "old",
		Error:                   &liveError,
		DeviceMAC:               "aa:bb:cc:dd:ee:ff",
		RedistributeToSdWAN:     true,
		RedistributeToSdWANType: "e1",
		UploadedFileName:        "old.conf",
	}

	plan := &bgpResourceModel{
		Enabled:  types.BoolValue(true),
		Config:   types.StringValue("router bgp 65000\n bgp router-id 192.168.200.1"),
		ASN:      types.Int64Null(),
		RouterID: types.StringNull(),
		Peers: types.ListNull(
			types.ObjectType{AttrTypes: bgpPeerModel{}.AttributeTypes()},
		),
		UploadFileName: types.StringValue("bgpd.conf"),
		Description:    types.StringValue("new"),
	}

	got, diags := r.planOntoBGP(ctx, plan, live)
	if diags.HasError() {
		t.Fatalf("planOntoBGP() diagnostics: %v", diags)
	}
	want := &unifi.BGPConfig{
		ID:                      "6ac2ccc4535c0e4cd8a8e1a2",
		Config:                  "router bgp 65000\n bgp router-id 192.168.200.1",
		Description:             "new",
		Enabled:                 true,
		DeviceMAC:               "aa:bb:cc:dd:ee:ff",
		RedistributeToSdWAN:     true,
		RedistributeToSdWANType: "e1",
		UploadedFileName:        "bgpd.conf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("planOntoBGP() = %+v, want %+v", got, want)
	}
	if live.Error == nil || !*live.Error || live.Description != "old" {
		t.Errorf("planOntoBGP changed its base: %+v", live)
	}
}

// The controller replaces the whole WAN network on update. planOntoNetwork lays the model over
// the network as the controller holds it, so the keys the resource does not declare (the
// firewall zone, the routing table, the controller's own UUID, the uplink identity) keep their
// live value, while a declared field takes the planned value.
func Test_wanResource_planOntoNetwork_keepsLiveKeys(t *testing.T) {
	ctx := context.Background()
	r := &wanResource{}

	zone := "zone-ext"
	table := int64(201)
	uplink := "eth4"
	tether := false
	live := &unifi.Network{
		ID:              "6a0001",
		Name:            new("Internet 1"),
		Purpose:         unifi.PurposeWAN,
		WANNetworkGroup: new("WAN"),
		Enabled:         true,
		ExternalID:      "ext-1",
		FirewallZoneID:  &zone,
		RoutingTableID:  &table,
		UplinkIDentity:  &uplink,
		IsWifiTethering: &tether,
		WANType:         new("dhcp"),
	}

	plan := &wanResourceModel{
		Name:         types.StringValue("Internet 1"),
		Enabled:      types.BoolValue(true),
		NetworkGroup: types.StringValue("WAN"),
		Type:         types.StringValue("pppoe"),
	}

	got, diags := r.planOntoNetwork(ctx, plan, live)
	if diags.HasError() {
		t.Fatalf("planOntoNetwork() diagnostics: %v", diags)
	}
	if got.WANType == nil || *got.WANType != "pppoe" {
		t.Errorf("WANType = %v, want the planned type", got.WANType)
	}
	if got.ExternalID != "ext-1" || got.FirewallZoneID == nil || *got.FirewallZoneID != "zone-ext" {
		t.Errorf("external_id or the firewall zone lost its live value: %+v", got)
	}
	if got.RoutingTableID == nil || *got.RoutingTableID != 201 {
		t.Errorf("RoutingTableID = %v, want the live 201", got.RoutingTableID)
	}
	if got.UplinkIDentity == nil || *got.UplinkIDentity != "eth4" {
		t.Errorf("UplinkIDentity = %v, want the live eth4", got.UplinkIDentity)
	}
	if got.IsWifiTethering == nil || *got.IsWifiTethering {
		t.Errorf("IsWifiTethering = %v, want the live false", got.IsWifiTethering)
	}
	if live.WANType == nil || *live.WANType != "dhcp" {
		t.Errorf("planOntoNetwork changed its base: %+v", live)
	}

	fresh, diags := r.planOntoNetwork(ctx, plan, nil)
	if diags.HasError() {
		t.Fatalf("planOntoNetwork(nil) diagnostics: %v", diags)
	}
	if fresh.ExternalID != "" || fresh.FirewallZoneID != nil || fresh.RoutingTableID != nil {
		t.Errorf("a create must start from an empty network: %+v", fresh)
	}
}
