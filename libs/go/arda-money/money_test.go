package ardamoney

import (
	"testing"

	"github.com/shopspring/decimal"
)

func mustEq(t *testing.T, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(MustFromString(want)) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestRoundCurrencyExponents(t *testing.T) {
	mustEq(t, Round(MustFromString("1250000.4"), "VND"), "1250000")
	mustEq(t, Round(MustFromString("1250000.5"), "VND"), "1250001")
	mustEq(t, Round(MustFromString("1234.567"), "USD"), "1234.57")
	mustEq(t, Round(MustFromString("1234.567"), "XXX"), "1234.57") // unknown → 2
}

func TestMonthlyInterest(t *testing.T) {
	// 500,000,000 VND at 8.5%/year → 3,541,666.67 → 3,541,667 VND
	interest := MonthlyInterest(MustFromString("500000000"), MustFromString("8.5"), "VND")
	mustEq(t, interest, "3541667")

	// 10,000.00 USD at 12%/year → 100.00 USD
	interest = MonthlyInterest(MustFromString("10000"), MustFromString("12"), "USD")
	mustEq(t, interest, "100.00")
}

func TestAllocateEvenSumsExactly(t *testing.T) {
	// 1,000,000 VND over 3 rows → 333334 + 333333 + 333333
	shares := AllocateEven(MustFromString("1000000"), 3, "VND")
	mustEq(t, shares[0], "333334")
	mustEq(t, shares[1], "333333")
	mustEq(t, shares[2], "333333")

	sum := decimal.Zero
	for _, s := range shares {
		sum = sum.Add(s)
	}
	mustEq(t, sum, "1000000")
}

func TestAllocateEvenNegativeTotal(t *testing.T) {
	shares := AllocateEven(MustFromString("-100.02"), 2, "USD")
	sum := decimal.Zero
	for _, s := range shares {
		sum = sum.Add(s)
	}
	mustEq(t, sum, "-100.02")
}

func TestExponentNormalisesCurrencyCode(t *testing.T) {
	if got := Exponent("vnd"); got != 0 {
		t.Fatalf("lowercase vnd exponent = %d, want 0", got)
	}
	if got := Exponent("  VND  "); got != 0 {
		t.Fatalf("padded VND exponent = %d, want 0", got)
	}
	if got := Exponent("kwd"); got != 3 {
		t.Fatalf("lowercase kwd exponent = %d, want 3", got)
	}
	for _, currency := range []string{"BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND"} {
		if got := Exponent(currency); got != 3 {
			t.Fatalf("%s exponent = %d, want 3", currency, got)
		}
	}
	mustEq(t, Round(MustFromString("1.0005"), "KWD"), "1.001")
}

func TestAllocateEvenRoundsTotalToMinorUnit(t *testing.T) {
	// 1.005 USD has sub-minor precision; it must be rounded to 1.01 before
	// splitting so no share carries a fraction of a cent.
	shares := AllocateEven(MustFromString("1.005"), 3, "USD")

	sum := decimal.Zero
	for _, s := range shares {
		if !s.Equal(Round(s, "USD")) {
			t.Fatalf("share %s is not a multiple of the USD minor unit", s)
		}
		sum = sum.Add(s)
	}
	mustEq(t, sum, "1.01")
}

func TestSubFloor(t *testing.T) {
	mustEq(t, SubFloor(MustFromString("50"), MustFromString("80")), "0")
	mustEq(t, SubFloor(MustFromString("80"), MustFromString("30")), "50")
}

func TestAllocateZeroParts(t *testing.T) {
	if shares := AllocateEven(MustFromString("100"), 0, "VND"); shares != nil {
		t.Fatalf("expected nil for zero parts, got %v", shares)
	}
}

func TestMinorUnitConversions(t *testing.T) {
	if Exponent("VND") != 0 || Exponent("USD") != 2 || Exponent("XYZ") != 2 {
		t.Fatalf("unexpected exponents: %d %d %d", Exponent("VND"), Exponent("USD"), Exponent("XYZ"))
	}
	// FromMinor: VND minor == major; USD cents → major.
	mustEq(t, FromMinor(3541667, "VND"), "3541667")
	mustEq(t, FromMinor(770055, "USD"), "7700.55")
	mustEq(t, FromMinor(-500, "EUR"), "-5.00")

	// ToMinor: rounds to exponent (half-away-from-zero), int64 both ways.
	if got, err := ToMinor(MustFromString("1234.567"), "USD"); err != nil || got != 123457 {
		t.Fatalf("got %d, %v; want 123457", got, err)
	}
	if got, err := ToMinor(MustFromString("-1234.5"), "USD"); err != nil || got != -123450 {
		t.Fatalf("got %d, %v; want -123450", got, err)
	}
	if got, err := ToMinor(MustFromString("500000000"), "VND"); err != nil || got != 500000000 {
		t.Fatalf("got %d, %v; want 500000000", got, err)
	}
	// Overflow guard.
	if _, err := ToMinor(MustFromString("99999999999999999999"), "VND"); err == nil {
		t.Fatal("expected overflow error")
	}

	// Round-trip identity on minor-exact values.
	for _, tc := range []struct {
		minor int64
		cur   string
	}{{3541667, "VND"}, {770055, "USD"}, {-9900, "JPY"}} {
		got, err := ToMinor(FromMinor(tc.minor, tc.cur), tc.cur)
		if err != nil || got != tc.minor {
			t.Fatalf("round trip %d %s: got %d, %v", tc.minor, tc.cur, got, err)
		}
	}
}
