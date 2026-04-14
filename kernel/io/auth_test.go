package io

import "testing"

func TestIdentityHasRole(t *testing.T) {
	id := &Identity{
		UserID: "u1",
		Roles:  []string{"admin", "editor"},
	}

	if !id.HasRole("admin") {
		t.Error("expected admin role to be found")
	}
	if !id.HasRole("editor") {
		t.Error("expected editor role to be found")
	}
	if id.HasRole("viewer") {
		t.Error("viewer role should not be present")
	}
}

func TestIdentityHasRole_Empty(t *testing.T) {
	id := &Identity{UserID: "u2"}
	if id.HasRole("admin") {
		t.Error("empty roles should return false")
	}
}
