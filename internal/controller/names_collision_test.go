package controller

import "testing"

// Two Restores in one namespace must not map to the same restored PVC name.
func TestClaimNamesDoNotCollide(t *testing.T) {
	a := newTestRestore()
	a.Name = "db"

	b := newTestRestore()
	b.Name = "db-data"

	if na, nb := restoredClaimName(a, "data-x"), restoredClaimName(b, "x"); na == nb {
		t.Fatalf("Restore db/data-x and db-data/x both use PVC %q", na)
	}
}
