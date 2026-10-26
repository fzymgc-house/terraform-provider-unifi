# ACL rules can be imported using the rule ID, or site:id for a non-default site.
terraform import unifi_acl_rule.example 6c0a2f1e-0000-4000-8000-000000000001

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_acl_rule.example default:6c0a2f1e-0000-4000-8000-000000000001
