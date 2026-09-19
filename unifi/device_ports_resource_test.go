package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// devicePortsTestMAC is a simulated switch the demo-mode controller provides.
// These tests own it; the unifi_device tests use 00:27:22:00:00:02.
const devicePortsTestMAC = "00:27:22:00:00:05"

func devicePortsTestClient(t *testing.T) *unifi.ApiClient {
	t.Helper()
	c, err := unifi.New(context.Background(), &unifi.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: os.Getenv("UNIFI_INSECURE") == "true",
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func devicePortsTestDevice(t *testing.T, c *unifi.ApiClient) rawDevice {
	t.Helper()
	var body struct {
		Data []rawDevice `json:"data"`
	}
	if err := c.Do(
		context.Background(),
		http.MethodGet,
		"api/s/default/stat/device/"+devicePortsTestMAC,
		nil,
		&body,
	); err != nil {
		t.Fatalf("read device: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("device %s: %d records", devicePortsTestMAC, len(body.Data))
	}
	return body.Data[0]
}

func devicePortsTestPut(t *testing.T, c *unifi.ApiClient, entries []portOverrideEntry) {
	t.Helper()
	id := devicePortsTestDevice(t, c).ID
	body := map[string]any{"port_overrides": entries}
	if err := c.Do(
		context.Background(),
		http.MethodPut,
		"api/s/default/rest/device/"+id,
		body,
		nil,
	); err != nil {
		t.Fatalf("write port_overrides: %v", err)
	}
}

// devicePortsPreCheck adopts the test switch if needed and clears its port
// definitions, so each test starts from a device with none.
func devicePortsPreCheck(t *testing.T) {
	t.Helper()
	preCheck(t)
	c := devicePortsTestClient(t)
	ctx := context.Background()
	var body struct {
		Data []struct {
			Adopted   bool  `json:"adopted"`
			PortTable []any `json:"port_table"`
		} `json:"data"`
	}
	for range 40 {
		if err := c.Do(
			ctx,
			http.MethodGet,
			"api/s/default/stat/device/"+devicePortsTestMAC,
			nil,
			&body,
		); err != nil {
			t.Fatalf("read device: %v", err)
		}
		if len(body.Data) == 1 && body.Data[0].Adopted && len(body.Data[0].PortTable) > 0 {
			devicePortsTestPut(t, c, []portOverrideEntry{})
			return
		}
		if len(body.Data) == 1 && !body.Data[0].Adopted {
			if err := c.AdoptDevice(ctx, "default", devicePortsTestMAC); err != nil {
				t.Fatalf("adopt: %v", err)
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("device %s never reported an adopted port table", devicePortsTestMAC)
}

// testAccCheckDevicePortsReset asserts destroy left the device with no port
// definitions.
func testAccCheckDevicePortsReset(t *testing.T) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if n := len(devicePortsTestDevice(t, devicePortsTestClient(t)).PortOverrides); n != 0 {
			return fmt.Errorf("destroy left %d port definitions", n)
		}
		return nil
	}
}

// devicePortsRawEntry checks one key of one port_overrides entry as the
// controller holds it, independently of the provider's state.
func devicePortsRawEntry(t *testing.T, idx int64, key, want string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		for _, e := range devicePortsTestDevice(t, devicePortsTestClient(t)).PortOverrides {
			if i, err := e.index(); err == nil && i == idx {
				if got := string(e[key]); got != want {
					return fmt.Errorf("port %d %s = %s, want %s", idx, key, got, want)
				}
				return nil
			}
		}
		return fmt.Errorf("port %d has no entry", idx)
	}
}

func TestAccDevicePorts_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { devicePortsPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDevicePortsReset(t),
		Steps: []resource.TestStep{
			{
				Config: testAccDevicePortsConfig_create(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_device_ports.test", "id"),
					resource.TestCheckResourceAttr("unifi_device_ports.test", "ports.%", "4"),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.name",
						"acc-trunk",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.tagged_vlan_mgmt",
						"auto",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.2.tagged_vlan_mgmt",
						"block_all",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.5.op_mode",
						"aggregate",
					),
					resource.TestCheckResourceAttrSet("unifi_device_ports.test", "ports.5.lag_idx"),
					devicePortsRawEntry(t, 1, "forward", `"all"`),
					devicePortsRawEntry(t, 2, "forward", `"native"`),
				),
			},
			{
				Config: testAccDevicePortsConfig_update(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_device_ports.test", "ports.%", "3"),
					resource.TestCheckResourceAttrSet("unifi_device_ports.test", "ports.7.lag_idx"),
					devicePortsRawEntry(t, 1, "forward", `"native"`),
					resource.TestCheckNoResourceAttr("unifi_device_ports.test", "ports.5.op_mode"),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.name",
						"acc-trunk-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.2.tagged_vlan_mgmt",
						"custom",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.2.excluded_networkconf_ids.#",
						"1",
					),
					devicePortsRawEntry(t, 2, "forward", `"customize"`),
				),
			},
			// Renaming one port leaves every other declared port, including the
			// aggregation lead on port 7, planned with its prior values.
			{
				Config: testAccDevicePortsConfig_rename(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.name",
						"acc-trunk-renamed-again",
					),
					resource.TestCheckResourceAttrSet("unifi_device_ports.test", "ports.7.lag_idx"),
				),
			},
			// A second aggregation group, then the first one removed: the
			// remaining lead must keep its lag_idx or the apply is inconsistent.
			{
				Config: testAccDevicePortsConfig_twoLags(),
				Check: resource.TestCheckResourceAttrSet(
					"unifi_device_ports.test",
					"ports.9.lag_idx",
				),
			},
			{
				Config: testAccDevicePortsConfig_dropFirstLag(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("unifi_device_ports.test", "ports.7.op_mode"),
					resource.TestCheckResourceAttrSet("unifi_device_ports.test", "ports.9.lag_idx"),
				),
			},
			// The same MAC spelled with dashes must not replace the resource:
			// a replacement resets every port on the device.
			{
				Config: testAccDevicePortsConfig_dropFirstLagMAC("00-27-22-00-00-05"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"unifi_device_ports.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
			},
			{
				Config: testAccDevicePortsConfig_dropFirstLag(),
			},
			{
				ResourceName:      "unifi_device_ports.test",
				ImportState:       true,
				ImportStateId:     devicePortsTestMAC,
				ImportStateVerify: true,
			},
			{
				ResourceName:            "unifi_device_ports.test",
				ImportState:             true,
				ImportStateId:           "default:00-27-22-00-00-05",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"mac"},
			},
			{
				ResourceName:    "unifi_device_ports.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   devicePortsTestMAC,
			},
		},
	})
}

// TestAccDevicePorts_keepsUndeclaredKeys proves a write starts from the live
// entry: a key set on the controller and absent from the configuration
// survives an update of another key on the same port.
func TestAccDevicePorts_keepsUndeclaredKeys(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { devicePortsPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDevicePortsReset(t),
		Steps: []resource.TestStep{
			{
				Config: testAccDevicePortsConfig_single("acc-keep"),
			},
			{
				PreConfig: func() {
					c := devicePortsTestClient(t)
					entries := devicePortsTestDevice(t, c).PortOverrides
					for _, e := range entries {
						if i, _ := e.index(); i == 1 {
							e["poe_mode"] = json.RawMessage(`"off"`)
							e["isolation"] = json.RawMessage(`true`)
						}
					}
					devicePortsTestPut(t, c, entries)
				},
				Config: testAccDevicePortsConfig_single("acc-keep-renamed"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.name",
						"acc-keep-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_device_ports.test",
						"ports.1.poe_mode",
						"off",
					),
					devicePortsRawEntry(t, 1, "poe_mode", `"off"`),
					devicePortsRawEntry(t, 1, "isolation", `true`),
				),
			},
		},
	})
}

func TestAccDevicePorts_createRefusesExistingDefinitions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			devicePortsPreCheck(t)
			devicePortsTestPut(t, devicePortsTestClient(t), []portOverrideEntry{
				{"port_idx": json.RawMessage(`3`), "name": json.RawMessage(`"set-in-the-ui"`)},
			})
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			devicePortsTestPut(t, devicePortsTestClient(t), []portOverrideEntry{})
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      testAccDevicePortsConfig_single("acc-refused"),
				ExpectError: regexp.MustCompile(`already has port definitions`),
			},
		},
	})
}

func TestAccDevicePorts_rejectsDeclaredLagMember(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "5" = { op_mode = "aggregate", aggregate_members = [5, 6] }
    "6" = { name = "member" }
  }
}
`, devicePortsTestMAC),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Link aggregation member declared as a port`),
			},
			{
				Config: testAccDevicePortsConfig_lag(
					`op_mode = "aggregate", aggregate_members = [6, 7]`,
				),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Aggregation group without its lead`),
			},
			{
				Config: testAccDevicePortsConfig_lag(
					`op_mode = "switch", aggregate_members = [5, 6]`,
				),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Aggregation members on a non-aggregate port`),
			},
		},
	})
}

func testAccDevicePortsConfig_single(name string) string {
	return fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = %q }
  }
}
`, devicePortsTestMAC, name)
}

func testAccDevicePortsNetworks() string {
	return `
resource "unifi_network" "ports_a" {
  name   = "acc-ports-a"
  subnet = "192.168.61.1/24"
  vlan   = 61
}

resource "unifi_network" "ports_b" {
  name   = "acc-ports-b"
  subnet = "192.168.62.1/24"
  vlan   = 62
}
`
}

func testAccDevicePortsConfig_create() string {
	return testAccDevicePortsNetworks() + fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = "acc-trunk", tagged_vlan_mgmt = "auto" }
    "2" = { name = "acc-access", tagged_vlan_mgmt = "block_all" }
    "5" = { name = "acc-lag", op_mode = "aggregate", aggregate_members = [5, 6] }
    "7" = { name = "acc-becomes-lag" }
  }
}
`, devicePortsTestMAC)
}

func testAccDevicePortsConfig_update() string {
	return testAccDevicePortsNetworks() + fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = "acc-trunk-renamed", tagged_vlan_mgmt = "block_all" }
    "7" = { name = "acc-becomes-lag", op_mode = "aggregate", aggregate_members = [7, 8] }
    "2" = {
      name                     = "acc-access"
      tagged_vlan_mgmt         = "custom"
      excluded_networkconf_ids = [unifi_network.ports_a.id]
    }
  }
}
`, devicePortsTestMAC)
}

func testAccDevicePortsConfig_rename() string {
	return testAccDevicePortsNetworks() + fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = "acc-trunk-renamed-again", tagged_vlan_mgmt = "block_all" }
    "7" = { name = "acc-becomes-lag", op_mode = "aggregate", aggregate_members = [7, 8] }
    "2" = {
      name                     = "acc-access"
      tagged_vlan_mgmt         = "custom"
      excluded_networkconf_ids = [unifi_network.ports_a.id]
    }
  }
}
`, devicePortsTestMAC)
}

func testAccDevicePortsConfig_twoLags() string {
	return fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = "acc-trunk-renamed-again", tagged_vlan_mgmt = "block_all" }
    "7" = { name = "acc-becomes-lag", op_mode = "aggregate", aggregate_members = [7, 8] }
    "9" = { name = "acc-second-lag", op_mode = "aggregate", aggregate_members = [9, 10] }
  }
}
`, devicePortsTestMAC)
}

func testAccDevicePortsConfig_dropFirstLag() string {
	return testAccDevicePortsConfig_dropFirstLagMAC(devicePortsTestMAC)
}

func testAccDevicePortsConfig_dropFirstLagMAC(mac string) string {
	return fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "1" = { name = "acc-trunk-renamed-again", tagged_vlan_mgmt = "block_all" }
    "9" = { name = "acc-second-lag", op_mode = "aggregate", aggregate_members = [9, 10] }
  }
}
`, mac)
}

func testAccDevicePortsConfig_lag(port5 string) string {
	return fmt.Sprintf(`
resource "unifi_device_ports" "test" {
  mac = %q
  ports = {
    "5" = { %s }
  }
}
`, devicePortsTestMAC, port5)
}
