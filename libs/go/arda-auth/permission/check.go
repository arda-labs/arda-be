package permission

// Has checks whether permissions contain the requested code.
func Has(permissions []string, code string) bool {
	for _, p := range permissions {
		if p == code {
			return true
		}
	}
	return false
}

// HasAny returns true if the user has any of the required permissions.
func HasAny(permissions []string, codes ...string) bool {
	set := make(map[string]struct{}, len(permissions))
	for _, p := range permissions {
		set[p] = struct{}{}
	}
	for _, c := range codes {
		if _, ok := set[c]; ok {
			return true
		}
	}
	return false
}
