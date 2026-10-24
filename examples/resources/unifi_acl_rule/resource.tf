# Two MAC ACL rules hold one client to the gateway: the ALLOW rule admits its frames to the
# gateway and to broadcast, and the BLOCK rule drops every other frame from it. A switch
# evaluates the rules of a network in acl_index order.
#
# An empty source_macs is refused, because the controller reads it as every device.
# An empty switch_macs puts a rule on every switch.
resource "unifi_acl_rule" "allow" {
  name             = "client-to-gateway"
  action           = "ALLOW"
  acl_index        = 1
  network_id       = unifi_network.example.id
  switch_macs      = ["00:11:22:33:44:01"]
  source_macs      = ["02:00:00:00:00:10"]
  destination_macs = ["00:11:22:33:44:55", "ff:ff:ff:ff:ff:ff"]
}

resource "unifi_acl_rule" "block" {
  name             = "client-block"
  action           = "BLOCK"
  acl_index        = 2
  network_id       = unifi_network.example.id
  switch_macs      = ["00:11:22:33:44:01"]
  source_macs      = ["02:00:00:00:00:10"]
  destination_macs = []
}
