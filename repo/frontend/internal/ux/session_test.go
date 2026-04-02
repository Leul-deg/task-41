package ux

import "testing"

func TestScopedKey_NamespacesByUser(t *testing.T) {
	if got := ScopedKey("support:draft", "user-1"); got != "support:draft:user-1" {
		t.Fatalf("unexpected scoped key %s", got)
	}
	if got := ScopedKey("support:draft", ""); got != "support:draft:anon" {
		t.Fatalf("expected anon fallback, got %s", got)
	}
}

func TestRequiresSession_PathRules(t *testing.T) {
	if RequiresSession("/") {
		t.Fatal("root login route should not require session")
	}
	if RequiresSession("/hiring/kiosk") {
		t.Fatal("kiosk page should not require session")
	}
	if RequiresSession("/static/css/theme.css") {
		t.Fatal("static assets should not require session")
	}
	if RequiresSession("/rpc/login") {
		t.Fatal("rpc routes should not require session")
	}
	if !RequiresSession("/dashboard") {
		t.Fatal("dashboard should require session")
	}
}
