package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func aclRuleTestModel(t *testing.T, switches, sources, destinations []string) aclRuleResourceModel {
	t.Helper()
	ctx := context.Background()
	set := func(macs []string) types.Set {
		s, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{}, macs)
		require.False(t, d.HasError(), d)
		return s
	}
	return aclRuleResourceModel{
		Name:            types.StringValue("fence-block"),
		Description:     types.StringNull(),
		Enabled:         types.BoolValue(true),
		Action:          types.StringValue("BLOCK"),
		Index:           types.Int64Value(4),
		NetworkID:       types.StringValue("net-telework"),
		SwitchMACs:      set(switches),
		SourceMACs:      set(sources),
		DestinationMACs: set(destinations),
	}
}

// TestPlanOntoACLRuleKeepsStoredKeys lays a plan over a stored rule. The managed keys take the
// plan, the keys that the schema does not declare keep their stored values, and the MAC mask
// is cleared, as the UI clears it for a list of devices.
func TestPlanOntoACLRuleKeepsStoredKeys(t *testing.T) {
	index := int64(1)
	stored := &unifi.AclRule{
		ID:                "rule1",
		AclIndex:          &index,
		Action:            "ALLOW",
		Description:       "kept",
		Enabled:           false,
		ExternalID:        "ext1",
		IPAclProtocol:     "ALL",
		MACAclNetworkID:   "net-old",
		Name:              "old",
		OriginID:          "origin1",
		OriginType:        "object_acl_rule",
		Predefined:        true,
		SpecificEnforcers: []string{"02:00:00:00:00:aa"},
		TrafficSource: &unifi.AclRuleTrafficSource{
			Type:                 "CLIENT_MAC",
			SpecificMACAddresses: []string{"02:00:00:00:00:01"},
			MACMask:              "ff:ff:ff:00:00:00",
			NetworkIDs:           []string{"kept-net"},
		},
		TrafficDestination: &unifi.AclRuleTrafficDestination{
			Type:                 "CLIENT_MAC",
			SpecificMACAddresses: []string{"02:00:00:00:00:02"},
		},
		Type: "MAC",
	}
	model := aclRuleTestModel(t, nil, []string{"02-42-C0-A8-D9-0A"}, nil)

	got, d := planOntoACLRule(context.Background(), &model, stored)
	require.False(t, d.HasError(), d)

	body, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"_id": "rule1",
		"acl_index": 4,
		"action": "BLOCK",
		"description": "kept",
		"enabled": true,
		"external_id": "ext1",
		"ip_acl_protocol": "ALL",
		"mac_acl_network_id": "net-telework",
		"name": "fence-block",
		"origin_id": "origin1",
		"origin_type": "object_acl_rule",
		"predefined": true,
		"specific_enforcers": [],
		"traffic_source": {
			"network_ids": ["kept-net"],
			"specific_mac_addresses": ["02:42:c0:a8:d9:0a"],
			"type": "CLIENT_MAC"
		},
		"traffic_destination": {"specific_mac_addresses": [], "type": "CLIENT_MAC"},
		"type": "MAC"
	}`, string(body))
	assert.Equal(
		t,
		"ff:ff:ff:00:00:00",
		stored.TrafficSource.MACMask,
		"the stored rule must not change",
	)
}

// TestPlanOntoACLRuleRefusesEmptySource guards the write itself: the controller reads an empty
// source as every device.
func TestPlanOntoACLRuleRefusesEmptySource(t *testing.T) {
	model := aclRuleTestModel(t, nil, []string{}, nil)

	got, d := planOntoACLRule(context.Background(), &model, nil)
	assert.True(t, d.HasError())
	assert.Nil(t, got)
}

func testAccACLRuleCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_acl_rule" {
			continue
		}
		_, err := c.GetAclRule(ctx, "default", rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_acl_rule %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

// TestAccACLRule_basic creates the ALLOW and BLOCK pair of a MAC fence, changes the
// destinations of the ALLOW rule in place, and imports the BLOCK rule in each form.
func TestAccACLRule_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccACLRuleCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccACLRuleConfig(`["02:00:00:00:00:10", "ff:ff:ff:ff:ff:ff"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_acl_rule.allow", "id"),
					resource.TestCheckResourceAttr(
						"unifi_acl_rule.allow",
						"destination_macs.#",
						"2",
					),
					resource.TestCheckResourceAttr("unifi_acl_rule.allow", "switch_macs.#", "0"),
					resource.TestCheckResourceAttr("unifi_acl_rule.allow", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_acl_rule.block",
						"destination_macs.#",
						"0",
					),
					resource.TestCheckResourceAttr("unifi_acl_rule.block", "source_macs.#", "1"),
				),
			},
			{
				Config: testAccACLRuleConfig(
					`["02:00:00:00:00:10", "02:00:00:00:00:11", "ff:ff:ff:ff:ff:ff"]`,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_acl_rule.allow",
						"destination_macs.#",
						"3",
					),
				),
			},
			{
				ResourceName:      "unifi_acl_rule.block",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "unifi_acl_rule.block",
				ImportState:       true,
				ImportStateIdFunc: testAccAPGroupSitePrefixedID("unifi_acl_rule.block"),
				ImportStateVerify: true,
			},
			{
				ResourceName:    "unifi_acl_rule.block",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccACLRuleConfig(allowDestinations string) string {
	return fmt.Sprintf(`
data "unifi_network" "default" {
	name = "Default"
}

resource "unifi_acl_rule" "allow" {
	name             = "tfacc-acl-allow"
	action           = "ALLOW"
	acl_index        = 1
	network_id       = data.unifi_network.default.id
	switch_macs      = []
	source_macs      = ["02-42-C0-A8-D9-0A"]
	destination_macs = %s
}

resource "unifi_acl_rule" "block" {
	name             = "tfacc-acl-block"
	action           = "BLOCK"
	acl_index        = 2
	network_id       = data.unifi_network.default.id
	switch_macs      = []
	source_macs      = ["02:42:c0:a8:d9:0a"]
	destination_macs = []

	depends_on = [unifi_acl_rule.allow]
}
`, allowDestinations)
}
