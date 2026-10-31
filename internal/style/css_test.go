package style_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/style"
)

func TestCSS_Exact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   style.Style
		want string
	}{
		{name: "nothing set", in: style.Style{}, want: ""},
		{name: "only the mood box", in: style.Style{OwnMoodBox: true}, want: ""},
		{
			name: "one colour",
			in:   style.Style{Colors: map[string]string{"bg": "#ff00cc"}},
			want: "body {\n\t--s-bg: #ff00cc;\n}\n",
		},
		{
			name: "upper case is written in lower case",
			in:   style.Style{Colors: map[string]string{"link": "#00AAFF"}},
			want: "body {\n\t--s-link: #00aaff;\n}\n",
		},
		{
			name: "one font",
			in:   style.Style{HeadFont: "impact"},
			want: "body {\n\t--s-font-head: Impact, \"r-impact\", sans-serif;\n}\n",
		},
		{
			name: "Texto adds the soft text",
			in:   style.Style{Colors: map[string]string{"text": "#39ff14"}},
			want: "body {\n\t--s-text: #39ff14;\n" +
				"\t--s-text-soft: color-mix(in oklab, var(--s-text) 79%, var(--s-sheet));\n}\n",
		},
		{
			name: "Hoja adds the soft text too",
			in:   style.Style{Colors: map[string]string{"sheet": "#111111"}},
			want: "body {\n\t--s-sheet: #111111;\n" +
				"\t--s-text-soft: color-mix(in oklab, var(--s-text) 79%, var(--s-sheet));\n}\n",
		},
		{
			name: "all 7 colours and 2 fonts",
			in: style.Style{
				Colors: map[string]string{
					"bg": "#000001", "sheet": "#000002", "text": "#000003", "link": "#000004",
					"name": "#000005", "barbg": "#000006", "barfg": "#000007",
				},
				HeadFont: "impact", BodyFont: "courier",
			},
			want: "body {\n" +
				"\t--s-bg: #000001;\n" +
				"\t--s-sheet: #000002;\n" +
				"\t--s-text: #000003;\n" +
				"\t--s-link: #000004;\n" +
				"\t--s-name: #000005;\n" +
				"\t--s-bar-bg: #000006;\n" +
				"\t--s-bar-fg: #000007;\n" +
				"\t--s-font-head: Impact, \"r-impact\", sans-serif;\n" +
				"\t--s-font-body: \"Courier New\", Courier, \"r-courier\", monospace;\n" +
				"\t--s-text-soft: color-mix(in oklab, var(--s-text) 79%, var(--s-sheet));\n" +
				"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.CSS(); got != tt.want {
				t.Errorf("CSS() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestCSS_SlotOrderIsStable(t *testing.T) {
	t.Parallel()

	colors := map[string]string{}
	for i, slot := range style.Slots() {
		colors[slot.Key] = "#00000" + string(rune('1'+i))
	}
	st := style.Style{Colors: colors, HeadFont: "comic", BodyFont: "pixel"}
	first := st.CSS()
	for range 50 {
		if got := st.CSS(); got != first {
			t.Fatalf("CSS() changed between calls:\n%q\n%q", first, got)
		}
	}

	// The slots appear in Slots() order, then the fonts.
	last := -1
	for _, slot := range style.Slots() {
		i := strings.Index(first, slot.Property+":")
		if i <= last {
			t.Errorf("%s is at %d, not after %d: slots must follow Slots() order", slot.Property, i, last)
		}
		last = i
	}
	if i := strings.Index(first, style.HeadFontProperty+":"); i <= last {
		t.Errorf("%s is not after the colours", style.HeadFontProperty)
	}
	if strings.Index(first, style.BodyFontProperty+":") <= strings.Index(first, style.HeadFontProperty+":") {
		t.Errorf("%s is not after %s", style.BodyFontProperty, style.HeadFontProperty)
	}
}

// TestCSS_OnlyValidatedValues feeds the builder values that would break out of
// a declaration, and checks that every value in what it writes is a #rrggbb, a
// registry stack, or the soft-text constant.
func TestCSS_OnlyValidatedValues(t *testing.T) {
	t.Parallel()

	evil := style.Style{
		Colors: map[string]string{
			"bg":      "#ff0000;} body{display:none",
			"sheet":   "red",
			"text":    "#12345",
			"link":    "#1234567",
			"nope":    "#abcdef",
			"name":    "",
			"barbg":   "url(https://evil.example/x.png)",
			"barfg":   "#00ff00",
			"unknown": "</style><script>",
		},
		HeadFont: "x; background: url(//evil)",
		BodyFont: "verdana",
	}
	got := evil.CSS()

	allowed := map[string]bool{"color-mix(in oklab, var(--s-text) 79%, var(--s-sheet))": true}
	for _, f := range style.Fonts() {
		allowed[f.Stack] = true
	}
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if lines[0] != "body {" || lines[len(lines)-1] != "}" {
		t.Fatalf("CSS() = %q, want one body rule", got)
	}
	for _, line := range lines[1 : len(lines)-1] {
		prop, value, ok := strings.Cut(strings.TrimSuffix(strings.TrimSpace(line), ";"), ": ")
		if !ok {
			t.Fatalf("declaration %q has no value", line)
		}
		if !hex.MatchString(value) && !allowed[value] {
			t.Errorf("%s has value %q: neither a validated hex nor a registry stack", prop, value)
		}
	}
	if !strings.Contains(got, "--s-bar-fg: #00ff00;") || !strings.Contains(got, "--s-font-body:") {
		t.Errorf("CSS() = %q, want the valid colour and font kept", got)
	}
	if strings.Contains(got, "evil") || strings.Contains(got, "<") || strings.Count(got, "{") != 1 {
		t.Errorf("CSS() = %q, want none of the invalid input", got)
	}
}

func TestStyle_IsDefaultAndEqual(t *testing.T) {
	t.Parallel()

	if !(style.Style{}).IsDefault() {
		t.Error("the zero Style is not default")
	}
	for name, st := range map[string]style.Style{
		"a colour":     {Colors: map[string]string{"bg": "#000000"}},
		"a head font":  {HeadFont: "comic"},
		"a body font":  {BodyFont: "comic"},
		"the mood box": {OwnMoodBox: true},
	} {
		if st.IsDefault() {
			t.Errorf("a style with %s is default", name)
		}
	}

	a := style.Style{Colors: map[string]string{"bg": "#000000"}, HeadFont: "comic"}
	b := style.Style{Colors: map[string]string{"bg": "#000000"}, HeadFont: "comic"}
	if !a.Equal(b) {
		t.Error("equal styles are not Equal")
	}
	b.Colors["bg"] = "#000001"
	if a.Equal(b) {
		t.Error("different styles are Equal")
	}
	if !(style.Style{}).Equal(style.Style{Colors: map[string]string{}}) {
		t.Error("nil and empty colours are not Equal")
	}
}
