package cccd

import "time"

type dates struct {
	dob, issue, expiry *time.Time
	noExpiry           bool
}

// validate adds flags; it never changes values. today is the current
// Vietnam calendar date at UTC midnight.
func validate(f *Fields, d dates, today time.Time) {
	checkIDNumber(f, d.dob)
	if d.dob != nil && d.dob.After(today) {
		f.DateOfBirth.AddFlag(FlagDateOrder)
	}
	if d.dob != nil && d.issue != nil && !d.dob.Before(*d.issue) {
		f.DateOfBirth.AddFlag(FlagDateOrder)
		f.IssueDate.AddFlag(FlagDateOrder)
	}
	if d.issue != nil && d.issue.After(today) {
		f.IssueDate.AddFlag(FlagDateOrder)
	}
	if d.noExpiry || d.expiry == nil {
		return
	}
	if d.issue != nil && !d.expiry.After(*d.issue) {
		f.ExpiryDate.AddFlag(FlagDateOrder)
		f.IssueDate.AddFlag(FlagDateOrder)
	}
	if d.expiry.Before(today) {
		f.ExpiryDate.AddFlag(FlagExpired)
	}
	// Cards expire on the holder's birthday at an age milestone.
	if d.dob != nil && (d.expiry.Month() != d.dob.Month() || d.expiry.Day() != d.dob.Day()) {
		f.ExpiryDate.AddFlag(FlagExpiryInconsistent)
	}
}

// checkIDNumber: digits 1-3 province code, digit 4 sex+century
// (0/1 = male/female born 1900s, 2/3 = 2000s, ... 8/9 = 2300s), digits 5-6
// the last two digits of the birth year.
func checkIDNumber(f *Fields, dob *time.Time) {
	if f.IDNumber.Value == nil {
		return
	}
	id := *f.IDNumber.Value
	if len(id) != 12 {
		f.IDNumber.AddFlag(FlagInvalidFormat)
		return
	}
	if !knownProvinceCode(id[:3]) {
		f.IDNumber.AddFlag(FlagUnknownProvinceCode)
	}
	digit := int(id[3] - '0')
	female := digit%2 == 1
	year := 1900 + 100*(digit/2) + int(id[4]-'0')*10 + int(id[5]-'0')
	if dob != nil && dob.Year() != year {
		f.IDNumber.AddFlag(FlagMismatchIDNumber)
		f.DateOfBirth.AddFlag(FlagMismatchIDNumber)
	}
	if f.Sex.Value != nil && (*f.Sex.Value == "F") != female {
		f.IDNumber.AddFlag(FlagMismatchIDNumber)
		f.Sex.AddFlag(FlagMismatchIDNumber)
	}
}
