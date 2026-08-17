package version

import "testing"

func TestVersion_IsSet(t *testing.T) {
	if Version == "" {
		t.Fatalf("expected a non-empty version string")
	}
}
