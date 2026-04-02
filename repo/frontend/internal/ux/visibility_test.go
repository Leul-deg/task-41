package ux

import "testing"

func TestConflictResolveURL_UsesTicketID(t *testing.T) {
	got := ConflictResolveURL("ticket-123")
	want := "/rpc/api/support/tickets/ticket-123/conflict-resolve"
	if got != want {
		t.Fatalf("expected %s got %s", want, got)
	}
}

func TestCanAccess_ByPermissionMap(t *testing.T) {
	perms := map[string]map[string]bool{
		"hiring": {"view": true, "create": false},
	}
	if !CanAccess(perms, "hiring", "view") {
		t.Fatal("expected access to hiring view")
	}
	if CanAccess(perms, "hiring", "create") {
		t.Fatal("did not expect access to hiring create")
	}
}

func TestModuleVisible_ViewOrCreate(t *testing.T) {
	perms := map[string]map[string]bool{
		"support": {"view": false, "create": true},
	}
	if !ModuleVisible(perms, "support") {
		t.Fatal("expected support module visible with create permission")
	}
	if ModuleVisible(perms, "compliance") {
		t.Fatal("did not expect compliance module visible")
	}
}
