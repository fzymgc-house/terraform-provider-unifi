# A device's port configuration can be imported by the device MAC address, e.g.
terraform import unifi_device_ports.core_switch 01:23:45:67:89:ab

# Or with an explicit site using the "site:mac" format.
terraform import unifi_device_ports.core_switch default:01:23:45:67:89:ab
