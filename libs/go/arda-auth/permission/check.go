package permission

import "slices"

// Has checks whether permissions contain the requested code.
// Super admin bypass: the "superadmin" sentinel permission checks true for anything.
func Has(permissions []string, code string) bool {
	return slices.Contains(permissions, "superadmin") || slices.Contains(permissions, code)
}

// HasAny returns true if the user has any of the required permissions.
// Super admin bypass: the "superadmin" sentinel permission grants everything.
func HasAny(permissions []string, codes ...string) bool {
	if slices.Contains(permissions, "superadmin") {
		return true
	}
	for _, c := range codes {
		if slices.Contains(permissions, c) {
			return true
		}
	}
	return false
}
