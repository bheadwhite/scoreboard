package auth

import "strings"

// Admins is the set of addresses that run the service.
//
// Deliberately not a column on users and not a family role. Family roles are
// about who runs a scoreboard; this is about who runs the deployment, and the
// two must not be confusable — an organizer who could grant themselves the
// power to erase results would undo the point of the concession protocol. It
// lives in the environment so that granting it takes a deploy.
//
// Empty is the safe default: a deployment that configures nobody has no
// administrators, rather than everybody.
type Admins map[string]struct{}

// NewAdmins parses the configured list.
//
// Commas and whitespace both separate, and whitespace is the form to prefer in a
// deployed environment: scripts/deploy.sh joins every plain environment variable
// into one comma-delimited --set-env-vars argument, so a comma inside this value
// would be read by gcloud as the start of another variable.
//
// Addresses are lowercased. Google reports them lowercased already, but a value
// typed into an env file by hand is not obliged to be.
func NewAdmins(raw string) Admins {
	out := Admins{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if email := strings.ToLower(strings.TrimSpace(field)); email != "" {
			out[email] = struct{}{}
		}
	}
	return out
}

// Has reports whether an address runs the service. Always false for an empty
// address, so a user row that somehow has no email cannot match an entry that
// was itself blank.
func (a Admins) Has(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	_, ok := a[email]
	return ok
}
