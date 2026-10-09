package sandbox

import "regexp"

// blankStringsAndComments replaces the contents of string literals, template
// literals and comments with spaces so identifier checks only see code. Length
// and line structure are preserved. It is a lexical approximation: a regex
// literal containing a quote can desynchronise it, which is why the static
// check is a second line of defence behind the runtime hardening.
func blankStringsAndComments(code string) string {
	runes := []rune(code)
	out := make([]rune, len(runes))
	copy(out, runes)
	blank := func(i int) {
		if i < len(out) && out[i] != '\n' {
			out[i] = ' '
		}
	}
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for ; i < len(runes) && runes[i] != '\n'; i++ {
				blank(i)
			}
		case c == '/' && i+1 < len(runes) && runes[i+1] == '*':
			blank(i)
			blank(i + 1)
			i += 2
			for ; i < len(runes) && !(runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '/'); i++ {
				blank(i)
			}
			blank(i)
			blank(i + 1)
			i++
		case c == '\'' || c == '"' || c == '`':
			quote := c
			i++
			for ; i < len(runes) && runes[i] != quote; i++ {
				if quote != '`' && runes[i] == '\n' {
					break
				}
				if runes[i] == '\\' && i+1 < len(runes) {
					blank(i)
					i++
				}
				blank(i)
			}
		}
	}
	return string(out)
}

// forbiddenBracketKeyPattern catches literal bracket access to dangerous
// members (x["constructor"], Object["defineProperty"]). Such a key is code
// even though it is written as a string, so it is checked on the original
// source before strings are blanked.
var forbiddenBracketKeyPattern = regexp.MustCompile(
	`\[\s*['"` + "`" + `]\s*(__proto__|constructor|prototype|defineProperty|defineProperties|getPrototypeOf|setPrototypeOf|__defineGetter__|__defineSetter__|__lookupGetter__|__lookupSetter__)\s*['"` + "`" + `]\s*\]`,
)
