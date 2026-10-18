package unifi

import (
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Network 11 moved the controller database to PostgreSQL and re-keyed every object to a UUID.
// The v1 collections resolve the old id through legacy_id, so a network, a WLAN or a port
// profile reads by either id. The v2 collections do not: a firewall zone, an AP group or a
// firewall policy is read from its list, and nothing in the list carries the old id.

// legacyIDPattern matches an object id from before Network 11.
var legacyIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// isLegacyID reports whether id has the form of an id from before Network 11.
func isLegacyID(id string) bool {
	return legacyIDPattern.MatchString(id)
}

// legacyIDNotFoundDiagnostic explains a read by a pre-Network 11 id that found nothing. The
// resource stays in the state: a silent removal plans a second object with the same name, and
// for a zone that changes the firewall.
func legacyIDNotFoundDiagnostic(kind, id string) diag.Diagnostic {
	return diag.NewErrorDiagnostic(
		fmt.Sprintf("%s %s not found by its pre-Network 11 id", kind, id),
		fmt.Sprintf(
			"Network 11 re-keyed every object to a UUID, and the %s list carries no old id. "+
				"Remove the resource from the state and import it by the id the controller shows now. "+
				"If the object is gone, remove it from the state.",
			kind,
		),
	)
}
