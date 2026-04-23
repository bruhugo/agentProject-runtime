package types

import (
	"regexp"
)

var safeIDRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// IsSafeID checks if the ID contains only alphanumeric characters, hyphens, and underscores.
// This prevents path traversal and other injection attacks.
func IsSafeID(id string) bool {
	return safeIDRegex.MatchString(id)
}
