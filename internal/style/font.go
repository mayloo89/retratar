package style

// DefaultHeadFont and DefaultBodyFont are the registry keys a page uses when
// its owner has not picked a font. Their stacks are the ones theme.css gives
// the --s-font-head and --s-font-body slots.
const (
	DefaultHeadFont = "trebuchet"
	DefaultBodyFont = "verdana"
)

// FontFile is one self-hosted file of a font. Name is the content-hashed
// file name served at /fonts/<Name>.
type FontFile struct {
	Weight int
	Name   string
}

// Font is one choice in the tipografía select.
type Font struct {
	// Key is the name used in forms and URLs.
	Key string
	// Label is the name the editor shows.
	Label string
	// Stack is the CSS font-family list. A classic's system face comes first,
	// so a device that has it keeps the real one; the self-hosted look-alike,
	// the family "r-<Key>" declared in theme.css, comes before the generic
	// family.
	Stack string
	// Files are the woff2 files of the family, regular first.
	Files []FontFile
}

// Family returns the @font-face family name theme.css declares for f.
func (f Font) Family() string {
	return "r-" + f.Key
}

var fonts = []Font{
	{
		Key: "verdana", Label: "Verdana",
		Stack: `Verdana, Geneva, "DejaVu Sans", "r-verdana", sans-serif`,
		Files: []FontFile{{400, "verdana-regular.2b93f3.woff2"}, {700, "verdana-bold.c39cf9.woff2"}},
	},
	{
		Key: "trebuchet", Label: "Trebuchet",
		Stack: `"Trebuchet MS", "Lucida Grande", "r-trebuchet", sans-serif`,
		Files: []FontFile{{400, "trebuchet-regular.92117c.woff2"}, {700, "trebuchet-bold.8e483a.woff2"}},
	},
	{
		Key: "arial", Label: "Arial",
		Stack: `Arial, Helvetica, "r-arial", sans-serif`,
		Files: []FontFile{{400, "arial-regular.5e8744.woff2"}, {700, "arial-bold.6c49ee.woff2"}},
	},
	{
		Key: "georgia", Label: "Georgia",
		Stack: `Georgia, "r-georgia", serif`,
		Files: []FontFile{{400, "georgia-regular.147d0c.woff2"}, {700, "georgia-bold.98cb1f.woff2"}},
	},
	{
		Key: "times", Label: "Times",
		Stack: `"Times New Roman", Times, "r-times", serif`,
		Files: []FontFile{{400, "times-regular.a27fa8.woff2"}, {700, "times-bold.bdfa09.woff2"}},
	},
	{
		Key: "tahoma", Label: "Tahoma",
		Stack: `Tahoma, Geneva, "r-tahoma", sans-serif`,
		Files: []FontFile{{400, "tahoma-regular.5106a8.woff2"}, {700, "tahoma-bold.b3b1bc.woff2"}},
	},
	{
		Key: "courier", Label: "Courier",
		Stack: `"Courier New", Courier, "r-courier", monospace`,
		Files: []FontFile{{400, "courier-regular.54ed37.woff2"}, {700, "courier-bold.8e218f.woff2"}},
	},
	{
		Key: "comic", Label: "Comic",
		Stack: `"Comic Sans MS", "r-comic", cursive`,
		Files: []FontFile{{400, "comic-regular.28eec8.woff2"}, {700, "comic-bold.fb07f2.woff2"}},
	},
	{
		Key: "impact", Label: "Impact",
		Stack: `Impact, "r-impact", sans-serif`,
		Files: []FontFile{{400, "impact-regular.20ba96.woff2"}},
	},
	{
		Key: "pixel", Label: "Pixel 8 bits",
		Stack: `"r-pixel", monospace`,
		Files: []FontFile{{400, "pixel-regular.48d76f.woff2"}},
	},
	{
		Key: "terminal", Label: "Terminal",
		Stack: `"r-terminal", monospace`,
		Files: []FontFile{{400, "terminal-regular.cd3869.woff2"}},
	},
	{
		Key: "script", Label: "Cursiva",
		Stack: `"r-script", cursive`,
		Files: []FontFile{{400, "script-regular.90d292.woff2"}},
	},
	{
		Key: "marker", Label: "Marcador",
		Stack: `"r-marker", cursive`,
		Files: []FontFile{{400, "marker-regular.d8228f.woff2"}},
	},
	{
		Key: "typewriter", Label: "Máquina de escribir",
		Stack: `"r-typewriter", "Courier New", monospace`,
		Files: []FontFile{{400, "typewriter-regular.317376.woff2"}},
	},
	{
		Key: "gothic", Label: "Gótica",
		Stack: `"r-gothic", serif`,
		Files: []FontFile{{400, "gothic-regular.6d4789.woff2"}},
	},
	{
		Key: "round", Label: "Redonda",
		Stack: `"r-round", sans-serif`,
		Files: []FontFile{{400, "round-regular.5d7960.woff2"}, {700, "round-bold.39e40b.woff2"}},
	},
}

// Fonts returns the registry in the order the select shows it.
func Fonts() []Font {
	return append([]Font(nil), fonts...)
}

// FontByKey returns the font with the given key.
func FontByKey(key string) (Font, bool) {
	for _, f := range fonts {
		if f.Key == key {
			return f, true
		}
	}
	return Font{}, false
}
