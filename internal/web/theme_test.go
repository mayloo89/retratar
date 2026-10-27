package web_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// readTheme returns theme.css from the source tree. The tests below compare it
// with internal/style, so they read the file, not a served copy.
func readTheme(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("static/theme.css")
	if err != nil {
		t.Fatalf("read theme.css: %v", err)
	}
	return string(b)
}

var cssCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)

// cssRule is one innermost "selector { declarations }" block of a stylesheet.
type cssRule struct{ selector, body string }

var cssRuleRE = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// cssRules splits a stylesheet into its innermost blocks, comments removed. An
// @media wrapper is dropped; the rules inside it are returned on their own.
func cssRules(css string) []cssRule {
	css = cssCommentRE.ReplaceAllString(css, "")
	var rules []cssRule
	for _, m := range cssRuleRE.FindAllStringSubmatch(css, -1) {
		rules = append(rules, cssRule{selector: strings.Join(strings.Fields(m[1]), " "), body: m[2]})
	}
	return rules
}

// declared returns the value a rule gives a custom property, or "".
func declared(r cssRule, prop string) string {
	re := regexp.MustCompile(`(?:^|[;\s])` + regexp.QuoteMeta(prop) + `:\s*([^;]+)`)
	if m := re.FindStringSubmatch(r.body); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ruleMayReadMood reports whether a rule is allowed to name the mood colours,
// --paper or --ink: the constants, the mood rules, the slot defaults, the mood
// box reset and the mood label.
func ruleMayReadMood(selector string) bool {
	switch selector {
	case ":root", "body", "body:not(.own-mood-box) .blk-mood", ".blk-mood-label":
		return true
	}
	return slices.ContainsFunc(strings.Split(selector, ","), func(s string) bool {
		return strings.HasPrefix(strings.TrimSpace(s), ".mood-")
	})
}

var cssPropRE = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// TestThemeCSS_SlotsOnly keeps a forgotten rule from ignoring an owner's
// colours: only the places that define the defaults may read the mood.
func TestThemeCSS_SlotsOnly(t *testing.T) {
	banned := map[string]bool{"--mood-bg": true, "--mood-fg": true, "--paper": true, "--ink": true, "--ink-soft": true}
	for _, r := range cssRules(readTheme(t)) {
		if ruleMayReadMood(r.selector) {
			continue
		}
		for _, prop := range cssPropRE.FindAllString(r.body, -1) {
			if banned[prop] {
				t.Errorf("rule %q reads %s, want a slot (--s-*)", r.selector, prop)
			}
		}
	}
}
