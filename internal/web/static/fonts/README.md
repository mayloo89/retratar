# Self-hosted fonts

Every font a page can use is served by the app itself at `/fonts/<file>`, from
both hosts. There is no font CDN: `font-src 'self'` is the CSP on both hosts, and
a third-party font host would see every visitor's IP address. Phones also lack
the classic 2000s system fonts (iOS has no Comic Sans or Impact; Android has
almost none), so the list uses openly licensed look-alikes.

`internal/style` is the registry (key, label, stack, files). `theme.css` has one
`@font-face` per file. Tests keep the directory, the registry and `theme.css` in
sync (`TestFontRegistry`). A browser downloads a file only when a page's font
stack reaches it.

## File names and caching

Each file is named `<key>-<regular|bold>.<hash>.woff2`, where `<hash>` is the first
6 hex digits of the SHA-256 of the file. The hash is part of the committed name,
not computed at build time, so `GET /fonts/{file}` can send
`Cache-Control: public, max-age=31536000, immutable`: a changed font is a new
file name, so a stale copy can never be served. `TestFontRegistry` checks that
every name matches its content. The build script below prints the new names;
update `internal/style/font.go` and `theme.css` with them.

## Fonts

| Key | Label | Family | Licence | Licence text | woff2 KB |
|---|---|---|---|---|---|
| `verdana` | Verdana | DejaVu Sans 2.37 | Bitstream Vera licence | [dejavu-sans.txt](LICENSES/dejavu-sans.txt) | 10.2, 11.1 |
| `trebuchet` | Trebuchet | Fira Sans | SIL OFL 1.1 | [fira-sans.txt](LICENSES/fira-sans.txt) | 12.9, 12.7 |
| `arial` | Arial | Arimo | SIL OFL 1.1 | [arimo.txt](LICENSES/arimo.txt) | 11.8, 11.7 |
| `georgia` | Georgia | Gelasio | SIL OFL 1.1 | [gelasio.txt](LICENSES/gelasio.txt) | 14.2, 13.9 |
| `times` | Times | Liberation Serif 2.1.5 | SIL OFL 1.1 | [liberation-serif.txt](LICENSES/liberation-serif.txt) | 9.7, 9.7 |
| `tahoma` | Tahoma | PT Sans | SIL OFL 1.1 | [pt-sans.txt](LICENSES/pt-sans.txt) | 10.3, 10.1 |
| `courier` | Courier | Courier Prime | SIL OFL 1.1 | [courier-prime.txt](LICENSES/courier-prime.txt) | 10.6, 10.3 |
| `comic` | Comic | Comic Neue | SIL OFL 1.1 | [comic-neue.txt](LICENSES/comic-neue.txt) | 11.3, 11.6 |
| `impact` | Impact | Anton | SIL OFL 1.1 | [anton.txt](LICENSES/anton.txt) | 8.5 |
| `pixel` | Pixel 8 bits | Press Start 2P | SIL OFL 1.1 | [press-start-2p.txt](LICENSES/press-start-2p.txt) | 3.9 |
| `terminal` | Terminal | VT323 | SIL OFL 1.1 | [vt323.txt](LICENSES/vt323.txt) | 5.9 |
| `script` | Cursiva | Pacifico | SIL OFL 1.1 | [pacifico.txt](LICENSES/pacifico.txt) | 18.6 |
| `marker` | Marcador | Permanent Marker | Apache 2.0 | [permanent-marker.txt](LICENSES/permanent-marker.txt) | 28.3 |
| `typewriter` | Máquina de escribir | Special Elite | Apache 2.0 | [special-elite.txt](LICENSES/special-elite.txt) | 51.5 |
| `gothic` | Gótica | UnifrakturMaguntia | SIL OFL 1.1 | [unifrakturmaguntia.txt](LICENSES/unifrakturmaguntia.txt) | 19.2 |
| `round` | Redonda | Nunito | SIL OFL 1.1 | [nunito.txt](LICENSES/nunito.txt) | 12.6, 12.6 |

Total: 343 KB in 25 files, at most 52 KB per file. All licences
allow redistribution and embedding; each licence text is in `LICENSES/`. Fonts
with a single weight are declared for `400 700`, so the browser never fakes a bold.

Subsetting and renaming are modifications, and the SIL OFL does not let a modified
file keep a Reserved Font Name, so the build renames the internal family to
`r-<key>` (the name `theme.css` uses). The copyright notice inside each file is kept.

## Rebuilding

Needs Python 3 with `fonttools` and `brotli` (`pip install fonttools brotli`).
Download the source files into one directory under these names, from the sources
in the table above: the Google Fonts repository (raw files under
`https://raw.githubusercontent.com/google/fonts/main/<path>/`), DejaVu 2.37 and
Liberation 2.1.5 from their release pages.

| Source file | From |
|---|---|
| `dejavusans-regular.ttf`, `dejavusans-bold.ttf` | DejaVuSans.ttf, DejaVuSans-Bold.ttf |
| `firasans-regular.ttf`, `firasans-bold.ttf` | FiraSans-Regular.ttf, FiraSans-Bold.ttf |
| `arimo-var.ttf`, `gelasio-var.ttf`, `nunito-var.ttf` | Arimo[wght].ttf, Gelasio[wght].ttf, Nunito[wght].ttf |
| `liberationserif-regular.ttf`, `liberationserif-bold.ttf` | LiberationSerif-Regular.ttf, LiberationSerif-Bold.ttf |
| `ptsans-regular.ttf`, `ptsans-bold.ttf` | PT_Sans-Web-Regular.ttf, PT_Sans-Web-Bold.ttf |
| `courierprime-regular.ttf`, `courierprime-bold.ttf` | CourierPrime-Regular.ttf, CourierPrime-Bold.ttf |
| `comicneue-regular.ttf`, `comicneue-bold.ttf` | ComicNeue-Regular.ttf, ComicNeue-Bold.ttf |
| `anton-regular.ttf`, `pressstart2p-regular.ttf`, `vt323-regular.ttf`, `pacifico-regular.ttf` | Anton-Regular.ttf, PressStart2P-Regular.ttf, VT323-Regular.ttf, Pacifico-Regular.ttf |
| `permanentmarker-regular.ttf`, `specialelite-regular.ttf` | PermanentMarker-Regular.ttf, SpecialElite-Regular.ttf |
| `unifraktur-regular.ttf` | UnifrakturMaguntia-Book.ttf |

Each file is cut to Basic Latin, Latin-1 Supplement and General Punctuation
(`U+0020-007E,U+00A0-00FF,U+2000-206F`), so `áéíóúñü ¿¡ «» “” – — …` all render.
Variable fonts are pinned to one weight first. Save the two scripts next to each
other, then run `SRC=<sources dir> OUT=<this dir> bash build.sh`.

`rename.py`:

```python
"""Give a subset the internal family name r-<key>, so no Reserved Font Name
of the original survives in a modified file (SIL OFL 1.1, section 3)."""
import sys
from fontTools.ttLib import TTFont
path, key, weight = sys.argv[1:4]
font = TTFont(path, recalcBBoxes=False)
style = weight.capitalize()
names = {1: f"r-{key}", 2: style, 3: f"r-{key}-{style}", 4: f"r-{key} {style}", 6: f"r-{key}-{style}"}
table = font["name"]
table.names = [n for n in table.names if n.nameID not in (16, 17, 21, 22, 25)]
for n in table.names:
	if n.nameID in names:
		n.string = names[n.nameID]
font.flavor = "woff2"
font.save(path)
```

`build.sh`:

```sh
set -euo pipefail
SRC=${SRC:?dir with the source .ttf files}   # see the table in README.md
OUT=${OUT:-.}
UNI="U+0020-007E,U+00A0-00FF,U+2000-206F"
# sub <source.ttf> <key> <weight> [wght-axis-value]
sub() {
	local src=$1 key=$2 weight=$3 axis=${4:-} tmp
	tmp=$(mktemp -d)
	if [ -n "$axis" ]; then
		fonttools varLib.instancer "$src" "wght=$axis" -o "$tmp/in.ttf" -q
		src=$tmp/in.ttf
	fi
	pyftsubset "$src" --unicodes="$UNI" --flavor=woff2 --no-hinting --layout-features='kern,liga,calt' --output-file="$tmp/out.woff2"
	python3 "$(dirname "$0")/rename.py" "$tmp/out.woff2" "$key" "$weight"
	local hash
	hash=$(shasum -a 256 "$tmp/out.woff2" | cut -c1-6)
	mv "$tmp/out.woff2" "$OUT/$key-$weight.$hash.woff2"
	rm -rf "$tmp"
}
sub "$SRC/dejavusans-regular.ttf"        verdana    regular
sub "$SRC/dejavusans-bold.ttf"           verdana    bold
sub "$SRC/firasans-regular.ttf"          trebuchet  regular
sub "$SRC/firasans-bold.ttf"             trebuchet  bold
sub "$SRC/arimo-var.ttf"                 arial      regular 400
sub "$SRC/arimo-var.ttf"                 arial      bold    700
sub "$SRC/gelasio-var.ttf"               georgia    regular 400
sub "$SRC/gelasio-var.ttf"               georgia    bold    700
sub "$SRC/liberationserif-regular.ttf"   times      regular
sub "$SRC/liberationserif-bold.ttf"      times      bold
sub "$SRC/ptsans-regular.ttf"            tahoma     regular
sub "$SRC/ptsans-bold.ttf"               tahoma     bold
sub "$SRC/courierprime-regular.ttf"      courier    regular
sub "$SRC/courierprime-bold.ttf"         courier    bold
sub "$SRC/comicneue-regular.ttf"         comic      regular
sub "$SRC/comicneue-bold.ttf"            comic      bold
sub "$SRC/anton-regular.ttf"             impact     regular
sub "$SRC/pressstart2p-regular.ttf"      pixel      regular
sub "$SRC/vt323-regular.ttf"             terminal   regular
sub "$SRC/pacifico-regular.ttf"          script     regular
sub "$SRC/permanentmarker-regular.ttf"   marker     regular
sub "$SRC/specialelite-regular.ttf"      typewriter regular
sub "$SRC/unifraktur-regular.ttf"        gothic     regular
sub "$SRC/nunito-var.ttf"                round      regular 400
sub "$SRC/nunito-var.ttf"                round      bold    700
```
