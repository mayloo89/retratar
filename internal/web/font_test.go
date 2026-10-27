package web_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/style"
)

var fontFileRE = regexp.MustCompile(`^([a-z]+)-(regular|bold)\.([0-9a-f]{6})\.woff2$`)

func TestFontRegistry(t *testing.T) {
	css := readTheme(t)
	rules := cssRules(css)

	onDisk := map[string][]byte{}
	entries, err := os.ReadDir("static/fonts")
	if err != nil {
		t.Fatalf("read static/fonts: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".woff2" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("static/fonts", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		onDisk[e.Name()] = b
	}

	// What theme.css declares: file name -> font-family.
	declaredFiles := map[string]string{}
	for _, r := range rules {
		if r.selector != "@font-face" {
			continue
		}
		family := strings.Trim(declared(r, "font-family"), `"`)
		m := regexp.MustCompile(`url\(/fonts/([^)]+)\)`).FindStringSubmatch(r.body)
		if m == nil {
			t.Errorf("@font-face for %q has no /fonts/ url", family)
			continue
		}
		declaredFiles[m[1]] = family
	}

	seenKeys := map[string]bool{}
	seenFiles := map[string]bool{}
	for _, f := range style.Fonts() {
		if seenKeys[f.Key] {
			t.Errorf("font key %q appears twice", f.Key)
		}
		seenKeys[f.Key] = true
		if f.Label == "" || len(f.Files) == 0 {
			t.Errorf("font %q has no label or no files", f.Key)
		}
		if !strings.Contains(f.Stack, `"`+f.Family()+`"`) {
			t.Errorf("font %q stack = %q, want it to name %q", f.Key, f.Stack, f.Family())
		}
		for _, file := range f.Files {
			seenFiles[file.Name] = true
			data, ok := onDisk[file.Name]
			if !ok {
				t.Errorf("font %q lists %s, which is not in static/fonts", f.Key, file.Name)
				continue
			}
			m := fontFileRE.FindStringSubmatch(file.Name)
			if m == nil || m[1] != f.Key {
				t.Errorf("font file %q does not match the name pattern <key>-<weight>.<hash>.woff2 for %q", file.Name, f.Key)
				continue
			}
			if want := map[string]int{"regular": 400, "bold": 700}[m[2]]; file.Weight != want {
				t.Errorf("%s weight = %d, want %d", file.Name, file.Weight, want)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:])[:6]; got != m[3] {
				t.Errorf("%s: content hash is %s, want the name to say so", file.Name, got)
			}
			if declaredFiles[file.Name] != f.Family() {
				t.Errorf("theme.css declares %s as family %q, want an @font-face for %q", file.Name, declaredFiles[file.Name], f.Family())
			}
		}
	}
	for name := range onDisk {
		if !seenFiles[name] {
			t.Errorf("static/fonts/%s is not in the registry", name)
		}
	}
	for name := range declaredFiles {
		if !seenFiles[name] {
			t.Errorf("theme.css declares /fonts/%s, which is not in the registry", name)
		}
	}

	for _, key := range []string{style.DefaultHeadFont, style.DefaultBodyFont} {
		if _, ok := style.FontByKey(key); !ok {
			t.Errorf("default font %q is not in the registry", key)
		}
	}
	if _, ok := style.FontByKey("nope"); ok {
		t.Error("FontByKey(nope) = ok, want not found")
	}

	// The slot defaults on <body> are the default fonts' stacks.
	for _, r := range rules {
		if r.selector != "body" || declared(r, "--s-bg") == "" {
			continue
		}
		for prop, key := range map[string]string{style.HeadFontProperty: style.DefaultHeadFont, style.BodyFontProperty: style.DefaultBodyFont} {
			f, _ := style.FontByKey(key)
			if got := declared(r, prop); got != f.Stack {
				t.Errorf("theme.css body %s = %q, want the %s stack %q", prop, got, key, f.Stack)
			}
		}
	}
}

func TestFontRoutes(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	for _, host := range []string{guestbookAppHost, "ana.retrat.ar"} {
		for _, f := range style.Fonts() {
			for _, file := range f.Files {
				want, err := os.ReadFile(filepath.Join("static/fonts", file.Name))
				if err != nil {
					t.Fatal(err)
				}
				resp := request(t, h, http.MethodGet, host, "/fonts/"+file.Name, nil)
				got := readBody(t, resp)
				if resp.StatusCode != http.StatusOK {
					t.Errorf("GET %s/fonts/%s status = %d, want 200", host, file.Name, resp.StatusCode)
					continue
				}
				if ct := resp.Header.Get("Content-Type"); ct != "font/woff2" {
					t.Errorf("%s %s Content-Type = %q, want font/woff2", host, file.Name, ct)
				}
				if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
					t.Errorf("%s %s Cache-Control = %q, want the immutable header", host, file.Name, cc)
				}
				if got != string(want) {
					t.Errorf("%s %s body differs from static/fonts/%s", host, file.Name, file.Name)
				}
			}
		}

		for _, path := range []string{
			"/fonts/nope.woff2",
			"/fonts/verdana-regular.000000.woff2",
			"/fonts/..%2ftheme.css",
			"/fonts/%2e%2e%2ftheme.css",
			"/fonts/..%2f..%2fapp.css",
			"/fonts/static%2ffonts%2fverdana-regular.2b93f3.woff2",
			"/fonts/",
			"/fonts/verdana-regular.2b93f3.woff2/",
		} {
			resp := request(t, h, http.MethodGet, host, path, nil)
			resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s%s status = %d, want 404", host, path, resp.StatusCode)
			}
		}
	}
}
