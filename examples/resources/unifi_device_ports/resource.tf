resource "unifi_network" "core" {
  name   = "Core"
  subnet = "192.168.40.1/24"
}

resource "unifi_network" "iot" {
  name   = "IoT"
  subnet = "192.168.30.1/24"
  vlan   = 30
}

# Every port listed is defined as configured. Every port not listed runs the
# controller's default configuration.
resource "unifi_device_ports" "core_switch" {
  mac = "01:23:45:67:89:ab"

  ports = {
    # A trunk: Core untagged, every other network tagged.
    "12" = {
      name                  = "server-trunk"
      native_networkconf_id = unifi_network.core.id
      tagged_vlan_mgmt      = "auto"
    }

    # An access port on IoT with nothing tagged.
    "8" = {
      name                  = "sensor-hub"
      native_networkconf_id = unifi_network.iot.id
      tagged_vlan_mgmt      = "block_all"
      poe_mode              = "off"
    }

    # A link aggregation group, declared on its lead port only.
    "23" = {
      name              = "uplink"
      op_mode           = "aggregate"
      aggregate_members = [23, 24]
    }
  }

  lifecycle {
    # Destroying this resource resets every port on the switch.
    prevent_destroy = true
  }
}
