package indicator

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ParseAccountFormula converts one PCF accounting formula into the
// account_balance block the engine evaluates.
//
// The source formulas are signed sums over account-code prefixes with an
// occasional "if positive" condition, e.g.
//
//	DCN TK 10
//	DCC TK 139
//	DCN TK 30 - DCC TK 305
//	Tổng DCN các TK (10, 11, 13) - Tổng DCC các TK (139, 209)
//	DCC TK 7 - DCN TK 8
//	( DCN TK 5 - DCC TK 5 ) nếu DCN - DCC>0
//
// Anything outside that grammar returns an error and is NOT seeded: a
// regulatory balance sheet must never carry a guessed number. The caller is
// expected to log every rejection with its indicator code.
//
// accountPrefixRe is reused so a parsed prefix can never widen the SQL surface.
func ParseAccountFormula(formula string) (accountBalance, error) {
	raw := strings.TrimSpace(formula)
	raw = strings.Trim(raw, "[]")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return accountBalance{}, fmt.Errorf("empty formula")
	}

	// A hand-written formula that is prose ("Tự tính = …", "Trùng công thức
	// với …") cannot be parsed mechanically.
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "tự tính") || strings.Contains(lower, "trùng công thức") ||
		strings.HasPrefix(raw, "=") {
		return accountBalance{}, fmt.Errorf("formula is descriptive, not account-derived: %q", truncate(raw, 60))
	}

	// A trailing condition clamps the expression it attaches to. It is only
	// applied when the expression is a single top-level group: "A + B nếu X>0"
	// is ambiguous mechanically (does the clamp cover A only, B only, or both),
	// and a regulatory balance must not carry a guessed condition.
	expr := raw
	clampAll := ""
	condText := ""
	if idx := strings.Index(lower, "nếu"); idx >= 0 {
		condText = strings.TrimSpace(lower[idx+len("nếu"):])
		expr = strings.TrimSpace(raw[:idx])
	} else if m := trailingCmpRe.FindStringSubmatch(lower); m != nil {
		condText = m[1]
		expr = strings.TrimSpace(raw[:len(raw)-len(m[0])])
	}
	if condText != "" {
		switch {
		case strings.Contains(condText, ">"):
			clampAll = "positive"
		case strings.Contains(condText, "<"):
			clampAll = "negative"
		default:
			return accountBalance{}, fmt.Errorf("unrecognised condition %q", truncate(condText, 40))
		}
	}

	parts, _ := splitTopLevel(expr)
	if clampAll != "" && len(parts) > 1 {
		return accountBalance{}, fmt.Errorf("condition applies to an ambiguous multi-part expression: %q", truncate(raw, 60))
	}

	terms, err := parseSignedTerms(expr)
	if err != nil {
		return accountBalance{}, err
	}
	if len(terms) == 0 {
		return accountBalance{}, fmt.Errorf("no account terms parsed from %q", truncate(expr, 60))
	}
	if clampAll != "" {
		for i := range terms {
			terms[i].Clamp = clampAll
		}
	}
	return accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: terms}, nil
}

// trailingCmpRe catches a bare trailing comparison such as "… TK 51, 50 >0"
// where the source omitted "nếu".
var trailingCmpRe = regexp.MustCompile(`\s*(>\s*0|<\s*0)\s*$`)

// parseSignedTerms splits "A + B - C" at top level (parentheses respected) and
// parses each component, carrying its sign.
func parseSignedTerms(expr string) ([]accountTerm, error) {
	parts, signs := splitTopLevel(expr)
	terms := []accountTerm{}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// A parenthesised sub-expression is parsed recursively and its terms
		// inherit the outer sign.
		if strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") {
			inner, err := parseSignedTerms(strings.TrimSpace(part[1 : len(part)-1]))
			if err != nil {
				return nil, err
			}
			for j := range inner {
				inner[j].Sign = combineSign(signs[i], inner[j].Sign)
			}
			terms = append(terms, inner...)
			continue
		}
		term, ok, err := parseTerm(part)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("unrecognised term %q", truncate(part, 60))
		}
		term.Sign = combineSign(signs[i], term.Sign)
		terms = append(terms, term)
	}
	return terms, nil
}

// splitTopLevel splits on + and - that are not inside parentheses, returning
// the components and the sign each starts with (+1 or -1).
func splitTopLevel(expr string) ([]string, []int) {
	var parts []string
	var signs []int
	depth := 0
	current := strings.Builder{}
	sign := 1
	flush := func() {
		parts = append(parts, current.String())
		signs = append(signs, sign)
		current.Reset()
	}
	runes := []rune(expr)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '(':
			depth++
			current.WriteRune(r)
		case ')':
			depth--
			current.WriteRune(r)
		case '+':
			if depth == 0 {
				flush()
				sign = 1
				continue
			}
			current.WriteRune(r)
		case '-':
			if depth == 0 {
				flush()
				sign = -1
				continue
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	flush()
	// Drop the empty leading component produced by a leading sign.
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
		signs = signs[1:]
	}
	return parts, signs
}

var (
	// Side keywords appear with inconsistent Vietnamese diacritics in the
	// source ("Dư nợ" vs "Dự nợ"), so the vowel is a character class.
	sideKW = `(dcn|dcc|d[uưự]\s*n[oợ]|d[uưự]\s*c[oó])`
	// totalDCN: "Tổng DCN các TK (10, 11)" / "Tổng DCN TK 10, 11"
	totalRe = regexp.MustCompile(`(?i)^tổng\s+` + sideKW + `\s+(?:các\s+)?tk\s*[:(]?\s*([0-9,\s]+)\s*\)?$`)
	// single: "DCN TK 10" / "Dư nợ TK 31"
	singleRe = regexp.MustCompile(`(?i)^` + sideKW + `\s+tk\s+([0-9,\s]+)$`)
	// sides: "DCC các TK: 41512, 41513" (no "Tổng")
	sidesRe = regexp.MustCompile(`(?i)^` + sideKW + `\s+tk\s*[:(]\s*([0-9,\s]+)\s*\)?$`)
	// net alias: "Chênh lệch (DN-DC) TK 50, 51" means debit minus credit.
	netAliasRe = regexp.MustCompile(`(?i)^ch[êe]nh\s+l[ệe]ch\s*\(?\s*dn\s*[-–]\s*dc\s*\)?\s*tk\s+([0-9,\s]+)$`)
	// reversed net: "Chênh lệch DCC - DCN TK 51, 50" means credit minus debit.
	netRevRe = regexp.MustCompile(`(?i)^ch[êe]nh\s+l[ệe]ch\s+dcc\s*[-–]\s*dcn\s+tk\s+([0-9,\s]+)$`)
	// bare code list: "TK 10, 11"
	bareRe = regexp.MustCompile(`(?i)^(?:các\s+)?tk\s*[:(]?\s*([0-9,\s]+)\s*\)?$`)
	// range: "DCN TK 313004 đến 313011"
	rangeRe = regexp.MustCompile(`(?i)^` + sideKW + `\s+tk\s+([0-9]+)\s+đến\s+([0-9]+)$`)
	// exclusion group, with or without the "TK" token: "(trừ TK 366)" / "(trừ 819)"
	excludeGroupRe = regexp.MustCompile(`(?i)\(\s*trừ\s+(?:tk\s+)?([0-9]+)\s*\)`)
	// unparenthesised exclusion.
	excludeRe = regexp.MustCompile(`(?i)trừ\s+(?:tk\s+)?([0-9]+)`)
)

// maxRangeExpansion bounds a "X đến Y" account range so a typo can never ask
// for millions of prefixes.
const maxRangeExpansion = 100

// parseTerm parses one signed leaf group.
func parseTerm(part string) (accountTerm, bool, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return accountTerm{}, false, nil
	}

	// Exclusions ("… (trừ TK 366)") narrow the prefix list they attach to; drop
	// the clause before matching the account grammar.
	var excludes []string
	for _, m := range excludeGroupRe.FindAllStringSubmatch(part, -1) {
		excludes = append(excludes, m[1])
	}
	part = strings.TrimSpace(excludeGroupRe.ReplaceAllString(part, ""))
	for _, m := range excludeRe.FindAllStringSubmatch(part, -1) {
		excludes = append(excludes, m[1])
	}
	part = strings.TrimSpace(excludeRe.ReplaceAllString(part, ""))

	if m := rangeRe.FindStringSubmatch(part); m != nil {
		codes, err := expandRange(m[2], m[3])
		if err != nil {
			return accountTerm{}, false, err
		}
		return accountTerm{Side: sideFor(m[1]), Prefixes: codes, Exclude: excludes}, true, nil
	}
	if m := netAliasRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[1])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		return accountTerm{Side: "net", Prefixes: codes, Exclude: excludes}, true, nil
	}
	if m := netRevRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[1])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		neg := -1
		return accountTerm{Side: "net", Prefixes: codes, Exclude: excludes, Sign: &neg}, true, nil
	}
	if m := totalRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[2])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		return accountTerm{Side: sideFor(m[1]), Prefixes: codes, Exclude: excludes}, true, nil
	}
	if m := sidesRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[2])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		return accountTerm{Side: sideFor(m[1]), Prefixes: codes, Exclude: excludes}, true, nil
	}
	if m := singleRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[2])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		return accountTerm{Side: sideFor(m[1]), Prefixes: codes, Exclude: excludes}, true, nil
	}
	if m := bareRe.FindStringSubmatch(part); m != nil {
		codes := splitCodes(m[1])
		if len(codes) == 0 {
			return accountTerm{}, false, nil
		}
		// A bare "TK 5" in the source means the net of that account.
		return accountTerm{Side: "net", Prefixes: codes, Exclude: excludes}, true, nil
	}
	return accountTerm{}, false, nil
}

// expandRange turns "313004 đến 313011" into the inclusive prefix list. It
// refuses an unbounded or descending range.
func expandRange(from, to string) ([]string, error) {
	lo, err := strconv.Atoi(from)
	if err != nil {
		return nil, fmt.Errorf("bad range start %q", from)
	}
	hi, err := strconv.Atoi(to)
	if err != nil {
		return nil, fmt.Errorf("bad range end %q", to)
	}
	if hi < lo {
		return nil, fmt.Errorf("descending account range %s đến %s", from, to)
	}
	if hi-lo+1 > maxRangeExpansion {
		return nil, fmt.Errorf("account range %s đến %s expands beyond %d", from, to, maxRangeExpansion)
	}
	width := len(from)
	if len(to) > width {
		width = len(to)
	}
	out := make([]string, 0, hi-lo+1)
	for n := lo; n <= hi; n++ {
		out = append(out, fmt.Sprintf("%0*d", width, n))
	}
	return out, nil
}

func sideFor(keyword string) string {
	k := strings.ToLower(strings.Join(strings.Fields(keyword), " "))
	switch {
	case strings.HasPrefix(k, "dcn"):
		return "debit"
	default:
		// dcc and both "dư nợ" spellings resolve here via the character class;
		// only the "dư nợ" (with đ) form needs the đ-prefix check.
		if strings.HasPrefix(k, "dư nợ") || strings.HasPrefix(k, "dự nợ") || strings.HasPrefix(k, "du no") {
			return "debit"
		}
		return "credit"
	}
}

// splitCodes turns "10, 11, 13" (or "10 11 13") into a sorted, de-duplicated
// digit-prefix list, rejecting anything that is not a bare account prefix.
func splitCodes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	seen := map[string]bool{}
	out := []string{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !accountPrefixRe.MatchString(f) {
			continue
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func removeAll(codes, drop []string) []string {
	if len(drop) == 0 {
		return codes
	}
	dropSet := map[string]bool{}
	for _, d := range drop {
		dropSet[d] = true
	}
	out := codes[:0:0]
	for _, c := range codes {
		if !dropSet[c] {
			out = append(out, c)
		}
	}
	return out
}

func combineSign(outer int, inner *int) *int {
	v := 1
	if inner != nil {
		v = *inner
	}
	v *= outer
	if v == 1 {
		return nil
	}
	return &v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
