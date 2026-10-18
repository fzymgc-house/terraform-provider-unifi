package unifi

import "testing"

func TestIsLegacyID(t *testing.T) {
	t.Parallel()

	for id, want := range map[string]bool{
		"6abe625b1805676bbdd4ac3e":             true,
		"565019dd-7df8-48af-b215-ee65d9f46070": false,
		"default":                              false,
		"":                                     false,
		"6ABE625B1805676BBDD4AC3E":             false,
	} {
		if got := isLegacyID(id); got != want {
			t.Errorf("isLegacyID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestLegacyIDNotFoundDiagnosticNamesTheKind(t *testing.T) {
	t.Parallel()

	d := legacyIDNotFoundDiagnostic("firewall zone", "6abe625b1805676bbdd4ac3e")
	if d.Summary() != "firewall zone 6abe625b1805676bbdd4ac3e not found by its pre-Network 11 id" {
		t.Errorf("summary = %q", d.Summary())
	}
}
