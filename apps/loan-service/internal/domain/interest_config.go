package domain

// Interest configuration of a loan product (T9.1 step 3). Pure functions, no I/O.
//
// A product's interest settings live in lnm_product_interest_config as complete,
// versioned rows. ResolveInterestConfig picks the row in force for a day and
// ParseInterestConfig turns it into the typed InterestConfig the calculator
// consumes. Nothing is guessed on the way:
//
//   - every code is matched EXACTLY against a closed set (no substring match, no
//     trimming, no case folding), so "CM124.0021" is rejected, not read as
//     CM124.002;
//   - there is no '%' row and no default: a product with no version in force is
//     an error, and so is a version the calculator does not support;
//   - values the business has not confirmed (principal grace days, overdue rate)
//     stay nil in InterestConfig and are errors when a caller asks for them,
//     never 0.
//
// Precedence follows the platform parameter order ORG over TENANT (T2.1): the
// version in force of the agreement's org wins; an org without any version in
// force falls back to the tenant-wide version (org_code NULL), which has to
// have been entered on purpose. The version in force is chosen first and
// validated second, so an unsupported newer version is reported instead of
// being skipped in favour of an older one.

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Year-basis codes accepted by the configuration (com_cfg_base_int at EPAS).
const (
	InterestBaseCSTL01 = "CSTL_01" // "360/365": no confirmed meaning, never usable
	InterestBaseCSTL02 = "CSTL_02" // actual/365

	interestYearDaysCSTL02 = 365
)

// Codes of the product settings the calculator does not read as a flag but that
// the configuration still records.
const (
	InterestCalcMethodStandard  = "CM123.001"
	InterestCalcMethodAlt       = "CM123.002" // not supported
	InterestStartMethodStandard = "CM125.001"
	InterestStartMethodAlt      = "CM125.002" // not supported
	OverdueCheckAgreementDate   = "CM126.001"
	OverdueCheckRepayPlan       = "CM126.002" // not supported
	OverdueRateTypeOfInTerm     = "CM127.001" // overdue rate = % of the in-term rate
)

// Error codes of the configuration resolver. The year-basis codes are shared
// with the calculator (ErrCodeInterestBaseUndefined / ErrCodeInterestBaseUnknown).
const (
	// ErrCodeProductConfigRequestInvalid: the lookup itself is unusable (the
	// calculation date is missing or not an ISO YYYY-MM-DD business date).
	ErrCodeProductConfigRequestInvalid = "lnm.product_config.request_invalid"
	// ErrCodeProductConfigNotFound: no version of the product is in force on the
	// calculation date for the org or tenant-wide. There is no default.
	ErrCodeProductConfigNotFound = "lnm.product_config.not_found"
	// ErrCodeProductConfigInvalid: a stored value is outside its closed set or
	// the row is inconsistent (ambiguous versions, malformed date, half an
	// overdue-rate setting).
	ErrCodeProductConfigInvalid = "lnm.product_config.invalid"
	// ErrCodeInterestCalcMethodUnsupported: CM123.002.
	ErrCodeInterestCalcMethodUnsupported = "lnm.interest.calc_method_unsupported"
	// ErrCodeInterestStartMethodUnsupported: CM125.002.
	ErrCodeInterestStartMethodUnsupported = "lnm.interest.start_method_unsupported"
	// ErrCodeInterestOverdueCheckUnsupported: CM126.002.
	ErrCodeInterestOverdueCheckUnsupported = "lnm.interest.overdue_check_unsupported"
	// ErrCodeProductConfigGraceDaysMissing: principal grace days were asked for
	// but the version in force does not carry them.
	ErrCodeProductConfigGraceDaysMissing = "lnm.product_config.grace_days_missing"
	// ErrCodeProductConfigOverdueRateMissing: the overdue rate was asked for but
	// the version in force does not carry it.
	ErrCodeProductConfigOverdueRateMissing = "lnm.product_config.overdue_rate_missing"
)

// InterestConfigRecord is one stored lnm_product_interest_config row, exactly as
// persisted. EffectiveFrom is the ISO business date YYYY-MM-DD.
type InterestConfigRecord struct {
	ID                  string
	TenantID            string
	ProductCode         string
	OrgCode             *string // nil = tenant-wide
	EffectiveFrom       string
	InterestBaseCode    string
	InterestCalcMethod  string
	InterestDateMethod  string
	InterestStartMethod string
	OverdueCheckCond    string
	RoundNo             int
	RoundType           string
	PrincipalGraceDays  *int
	OverdueRateType     *string
	OverdueRateValue    *decimal.Decimal
}

// InterestConfig is a validated configuration version, ready for the calculator.
type InterestConfig struct {
	ConfigID      string
	OrgCode       string // "" = tenant-wide version
	EffectiveFrom string

	BaseCode   string // CSTL_02
	YearDays   int    // 365
	DateMethod string // CM124.001 / CM124.002
	RoundNo    int
	RoundType  string // CM163.001 / .002 / .003

	// Nil when the business has not confirmed the value; use GraceDays() and
	// OverdueRate() to get a coded error instead of a silent 0.
	PrincipalGraceDays *int
	OverdueRatePercent *decimal.Decimal
}

// GraceDays returns the principal grace days, or a coded error when the version
// in force does not carry them.
func (c InterestConfig) GraceDays() (int, error) {
	if c.PrincipalGraceDays == nil {
		return 0, &InterestError{Code: ErrCodeProductConfigGraceDaysMissing, Message: "principal grace days are not configured in version " + c.ConfigID}
	}
	return *c.PrincipalGraceDays, nil
}

// OverdueRate returns the overdue rate as a percentage of the in-term rate, or
// a coded error when the version in force does not carry it.
func (c InterestConfig) OverdueRate() (decimal.Decimal, error) {
	if c.OverdueRatePercent == nil {
		return decimal.Zero, &InterestError{Code: ErrCodeProductConfigOverdueRateMissing, Message: "overdue rate is not configured in version " + c.ConfigID}
	}
	return *c.OverdueRatePercent, nil
}

// ApplyTo copies the calculation settings of the version into a calculator input.
func (c InterestConfig) ApplyTo(in *InterestInput) {
	in.BaseCode = c.BaseCode
	in.DateMethod = c.DateMethod
	in.RoundNo = c.RoundNo
	in.RoundType = c.RoundType
}

func configInvalid(format string, args ...string) error {
	return &InterestError{Code: ErrCodeProductConfigInvalid, Message: format + strings.Join(args, " ")}
}

// ParseInterestConfig validates one stored version. Every code is compared as
// the exact string; the first problem found is returned.
func ParseInterestConfig(r InterestConfigRecord) (InterestConfig, error) {
	cfg := InterestConfig{ConfigID: r.ID, EffectiveFrom: r.EffectiveFrom}
	if r.OrgCode != nil {
		cfg.OrgCode = *r.OrgCode
	}

	switch r.InterestBaseCode {
	case InterestBaseCSTL02:
		cfg.BaseCode, cfg.YearDays = InterestBaseCSTL02, interestYearDaysCSTL02
	case InterestBaseCSTL01:
		return cfg, &InterestError{Code: ErrCodeInterestBaseUndefined, Message: "year-basis code CSTL_01 has no confirmed definition yet (version " + r.ID + ")"}
	default:
		return cfg, &InterestError{Code: ErrCodeInterestBaseUnknown, Message: "unknown year-basis code " + quoteCode(r.InterestBaseCode) + " (version " + r.ID + ")"}
	}

	switch r.InterestCalcMethod {
	case InterestCalcMethodStandard:
	case InterestCalcMethodAlt:
		return cfg, &InterestError{Code: ErrCodeInterestCalcMethodUnsupported, Message: "interest calc method CM123.002 is not supported yet (version " + r.ID + ")"}
	default:
		return cfg, configInvalid("interest_calc_method ", quoteCode(r.InterestCalcMethod), "(version", r.ID+")")
	}

	switch r.InterestDateMethod {
	case DateMethodSkipFirstDay, DateMethodSkipLastDay:
		cfg.DateMethod = r.InterestDateMethod
	default:
		return cfg, configInvalid("interest_date_method ", quoteCode(r.InterestDateMethod), "(version", r.ID+")")
	}

	switch r.InterestStartMethod {
	case InterestStartMethodStandard:
	case InterestStartMethodAlt:
		return cfg, &InterestError{Code: ErrCodeInterestStartMethodUnsupported, Message: "interest start method CM125.002 is not supported yet (version " + r.ID + ")"}
	default:
		return cfg, configInvalid("interest_start_method ", quoteCode(r.InterestStartMethod), "(version", r.ID+")")
	}

	switch r.OverdueCheckCond {
	case OverdueCheckAgreementDate:
	case OverdueCheckRepayPlan:
		return cfg, &InterestError{Code: ErrCodeInterestOverdueCheckUnsupported, Message: "overdue check CM126.002 is not supported yet (version " + r.ID + ")"}
	default:
		return cfg, configInvalid("overdue_check_cond ", quoteCode(r.OverdueCheckCond), "(version", r.ID+")")
	}

	switch r.RoundType {
	case RoundModeHalfUp, RoundModeCeiling, RoundModeFloor:
		cfg.RoundType = r.RoundType
	default:
		return cfg, configInvalid("round_type ", quoteCode(r.RoundType), "(version", r.ID+")")
	}
	if r.RoundNo < 0 {
		return cfg, configInvalid("round_no is negative (version ", r.ID+")")
	}
	cfg.RoundNo = r.RoundNo

	if r.PrincipalGraceDays != nil {
		if *r.PrincipalGraceDays < 0 {
			return cfg, configInvalid("principal_grace_days is negative (version ", r.ID+")")
		}
		d := *r.PrincipalGraceDays
		cfg.PrincipalGraceDays = &d
	}

	switch {
	case r.OverdueRateType == nil && r.OverdueRateValue == nil:
	case r.OverdueRateType == nil || r.OverdueRateValue == nil:
		return cfg, configInvalid("overdue rate type and value must be set together (version ", r.ID+")")
	case *r.OverdueRateType != OverdueRateTypeOfInTerm:
		return cfg, configInvalid("overdue_rate_type ", quoteCode(*r.OverdueRateType), "(version", r.ID+")")
	case r.OverdueRateValue.IsNegative():
		return cfg, configInvalid("overdue_rate_value is negative (version ", r.ID+")")
	default:
		v := *r.OverdueRateValue
		cfg.OverdueRatePercent = &v
	}
	return cfg, nil
}

// ResolveInterestConfig returns the configuration in force on asOf (ISO
// YYYY-MM-DD business date) for orgCode ("" = no org). records are all the
// stored versions of one (tenant, product): the org's and the tenant-wide ones;
// rows of any other org are ignored.
func ResolveInterestConfig(records []InterestConfigRecord, orgCode, asOf string) (InterestConfig, error) {
	if !isISODay(asOf) {
		return InterestConfig{}, &InterestError{Code: ErrCodeProductConfigRequestInvalid, Message: "calculation date must be an ISO YYYY-MM-DD business date, got " + quoteCode(asOf)}
	}

	var orgRows, tenantRows []InterestConfigRecord
	for _, r := range records {
		switch {
		case r.OrgCode == nil:
			tenantRows = append(tenantRows, r)
		case orgCode != "" && *r.OrgCode == orgCode:
			orgRows = append(orgRows, r)
		}
	}

	for _, scope := range [][]InterestConfigRecord{orgRows, tenantRows} {
		rec, found, err := versionInForce(scope, asOf)
		if err != nil {
			return InterestConfig{}, err
		}
		if found {
			return ParseInterestConfig(rec)
		}
	}
	return InterestConfig{}, &InterestError{Code: ErrCodeProductConfigNotFound, Message: "no interest configuration in force on " + asOf + " for org " + quoteCode(orgCode) + " or tenant-wide"}
}

// versionInForce picks, among the versions of one scope, the one with the
// latest effective_from not after asOf.
func versionInForce(rows []InterestConfigRecord, asOf string) (InterestConfigRecord, bool, error) {
	var best InterestConfigRecord
	found, tied := false, false
	for _, r := range rows {
		if !isISODay(r.EffectiveFrom) {
			return best, false, configInvalid("effective_from ", quoteCode(r.EffectiveFrom), "is not an ISO date (version", r.ID+")")
		}
		if r.EffectiveFrom > asOf { // ISO dates order lexically
			continue
		}
		switch {
		case !found || r.EffectiveFrom > best.EffectiveFrom:
			best, found, tied = r, true, false
		case r.EffectiveFrom == best.EffectiveFrom:
			tied = true
		}
	}
	if tied {
		return best, false, configInvalid("ambiguous interest configuration: several versions share effective_from ", best.EffectiveFrom)
	}
	return best, found, nil
}

// isISODay reports whether s is exactly a valid YYYY-MM-DD date.
func isISODay(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}
