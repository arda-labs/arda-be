package cccd

import (
	"slices"
	"testing"
	"time"
)

func d(s string) *time.Time {
	t, ok := parseDate(s)
	if !ok {
		panic(s)
	}
	return &t
}

func fieldsWith(id, sex string) Fields {
	return Fields{IDNumber: idField(id), Sex: sexField(sex), DateOfBirth: Field{Flags: []string{}},
		IssueDate: Field{Flags: []string{}}, ExpiryDate: Field{Flags: []string{}}}
}

var today = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

func TestIDNumberConsistency(t *testing.T) {
	cases := []struct {
		name, id, sex, dob          string
		idFlags, sexFlags, dobFlags []string
	}{
		{"male 1988 ok", "082088123456", "Nam", "26/05/1988", nil, nil, nil},
		{"female 2005 ok", "001305123456", "Nữ", "01/01/2005", nil, nil, nil},
		{"female 1960 ok", "046160123456", "Nữ", "22/12/1960", nil, nil, nil},
		{"leading zero dropped", "82279855585", "Nam", "26/05/1988", []string{FlagInvalidFormat}, nil, nil},
		{"sex mismatch", "082088123456", "Nữ", "26/05/1988", []string{FlagMismatchIDNumber}, []string{FlagMismatchIDNumber}, nil},
		{"year mismatch", "082088123456", "Nam", "26/05/1989", []string{FlagMismatchIDNumber}, nil, []string{FlagMismatchIDNumber}},
		{"century mismatch", "082288123456", "Nam", "26/05/1988", []string{FlagMismatchIDNumber}, nil, []string{FlagMismatchIDNumber}},
		{"unknown province", "099088123456", "Nam", "26/05/1988", []string{FlagUnknownProvinceCode}, nil, nil},
	}
	for _, c := range cases {
		f := fieldsWith(c.id, c.sex)
		var dob *time.Time
		f.DateOfBirth, dob = dateField(c.dob)
		validate(&f, dates{dob: dob}, today)
		for _, chk := range []struct {
			got, want []string
			what      string
		}{{f.IDNumber.Flags, c.idFlags, "id"}, {f.Sex.Flags, c.sexFlags, "sex"}, {f.DateOfBirth.Flags, c.dobFlags, "dob"}} {
			if !slices.Equal(chk.got, chk.want) && !(len(chk.got) == 0 && len(chk.want) == 0) {
				t.Errorf("%s: %s flags = %v, want %v", c.name, chk.what, chk.got, chk.want)
			}
		}
	}
}

func TestDateRules(t *testing.T) {
	type tc struct {
		name                         string
		d                            dates
		issueFlag, expiryFlag, dobFl []string
	}
	cases := []tc{
		{"all fine", dates{dob: d("26/05/1988"), issue: d("10/07/2021"), expiry: d("26/05/2028")}, nil, nil, nil},
		{"issue before birth", dates{dob: d("26/05/1988"), issue: d("10/07/1980"), expiry: d("26/05/2028")},
			[]string{FlagDateOrder}, nil, []string{FlagDateOrder}},
		{"issue in future", dates{dob: d("26/05/1988"), issue: d("10/07/2027"), expiry: d("26/05/2028")},
			[]string{FlagDateOrder}, nil, nil},
		{"expiry not after issue", dates{dob: d("26/05/1988"), issue: d("10/07/2021"), expiry: d("26/05/2020")},
			[]string{FlagDateOrder}, []string{FlagDateOrder, FlagExpired}, nil},
		{"expired", dates{dob: d("26/05/1988"), issue: d("10/07/2012"), expiry: d("26/05/2013")},
			nil, []string{FlagExpired}, nil},
		{"expiry day differs from birthday", dates{dob: d("26/05/1988"), issue: d("10/07/2021"), expiry: d("27/05/2028")},
			nil, []string{FlagExpiryInconsistent}, nil},
		{"no expiry skips expiry checks", dates{dob: d("26/05/1950"), issue: d("10/07/2021"), noExpiry: true}, nil, nil, nil},
		{"birth in future", dates{dob: d("01/01/2030")}, nil, nil, []string{FlagDateOrder}},
	}
	for _, c := range cases {
		f := Fields{IDNumber: Field{Flags: []string{}}, Sex: Field{Flags: []string{}},
			DateOfBirth: Field{Flags: []string{}}, IssueDate: Field{Flags: []string{}}, ExpiryDate: Field{Flags: []string{}}}
		validate(&f, c.d, today)
		eq := func(got, want []string) bool { return slices.Equal(got, want) || (len(got) == 0 && len(want) == 0) }
		if !eq(f.IssueDate.Flags, c.issueFlag) || !eq(f.ExpiryDate.Flags, c.expiryFlag) || !eq(f.DateOfBirth.Flags, c.dobFl) {
			t.Errorf("%s: issue %v expiry %v dob %v", c.name, f.IssueDate.Flags, f.ExpiryDate.Flags, f.DateOfBirth.Flags)
		}
	}
}

func TestKnownProvinceCode(t *testing.T) {
	for _, code := range []string{"001", "046", "079", "096"} {
		if !knownProvinceCode(code) {
			t.Errorf("%s should be known", code)
		}
	}
	for _, code := range []string{"000", "003", "097", "abc"} {
		if knownProvinceCode(code) {
			t.Errorf("%s should be unknown", code)
		}
	}
}
