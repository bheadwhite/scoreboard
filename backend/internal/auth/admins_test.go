package auth

import "testing"

func TestNewAdminsSeparators(t *testing.T) {
	// Whitespace is what a deployed value uses, because deploy.sh joins plain
	// environment variables with commas. Commas still have to work for a value
	// typed into .env.local by hand.
	for _, raw := range []string{
		"bill@example.com dad@example.com",
		"bill@example.com,dad@example.com",
		"  bill@example.com ,, dad@example.com  ",
		"bill@example.com\ndad@example.com",
	} {
		admins := NewAdmins(raw)
		if len(admins) != 2 {
			t.Errorf("NewAdmins(%q) parsed %d addresses, want 2", raw, len(admins))
		}
		if !admins.Has("bill@example.com") || !admins.Has("dad@example.com") {
			t.Errorf("NewAdmins(%q) lost an address: %v", raw, admins)
		}
	}
}

func TestAdminsIgnoreCase(t *testing.T) {
	admins := NewAdmins("Bill@Example.COM")
	if !admins.Has("bill@example.com") {
		t.Error("a configured address should match however it was typed")
	}
	if !admins.Has("  BILL@example.com  ") {
		t.Error("the address being checked should be trimmed and lowercased too")
	}
}

// The important negative. An unconfigured deployment must have no
// administrators, not everybody — including for a user row whose email is blank,
// which is what every dev-login and pre-OAuth account would look like.
func TestEmptyAdminsMatchNobody(t *testing.T) {
	for _, raw := range []string{"", "   ", ",", " , ; "} {
		admins := NewAdmins(raw)
		if len(admins) != 0 {
			t.Errorf("NewAdmins(%q) should be empty, got %v", raw, admins)
		}
		if admins.Has("bill@example.com") {
			t.Errorf("NewAdmins(%q) matched an address", raw)
		}
		if admins.Has("") {
			t.Errorf("NewAdmins(%q) matched the empty address", raw)
		}
	}
}

func TestAdminsRejectAnEmptyEmail(t *testing.T) {
	// A blank entry is dropped at parse time, so a user with no email on their
	// row cannot slip through by matching it.
	admins := NewAdmins("bill@example.com")
	if admins.Has("") {
		t.Error("the empty address must never be an administrator")
	}
	if admins.Has("dad@example.com") {
		t.Error("an address that was never configured must not match")
	}
}
