package currency

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalEncoding(t *testing.T) {
	b, e := encode(object{"z": "<é>", "a": 1})
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != "{\n  \"a\": 1,\n  \"z\": \"<é>\"\n}\n" {
		t.Fatalf("%s", b)
	}
}
func TestSynchronize(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{"nested/a.json": []byte("ok")}
	if e := synchronize(root, files, false); e != nil {
		t.Fatal(e)
	}
	if e := synchronize(root, files, true); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "nested/a.json"), []byte("bad"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := synchronize(root, files, true); e == nil {
		t.Fatal("accepted tampered data")
	}
	if e := os.WriteFile(filepath.Join(root, "extra"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := synchronize(root, files, false); e == nil {
		t.Fatal("accepted extra file")
	}
}
func TestUnsafePaths(t *testing.T) {
	for _, p := range []string{"../escape", "/absolute", "a/../escape", "a\\escape", ""} {
		if _, e := safePath(t.TempDir(), p); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
	root := t.TempDir()
	if e := os.Symlink(t.TempDir(), filepath.Join(root, "link")); e != nil {
		t.Skipf("symlink unsupported: %v", e)
	}
	if _, e := safePath(root, "link/file"); e == nil {
		t.Fatal("accepted symlink escape")
	}
}
func TestArguments(t *testing.T) {
	for _, args := range [][]string{{"--bad"}, {"unexpected"}, {"--app-check", "--check"}, {"--app-check", "--app", "x"}} {
		if e := Run(t.TempDir(), args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

// The fixture is deliberately synthetic: tests do not require sibling checkouts.
func backendFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sources := map[string]any{}
	currencies := map[string]int{}
	labels := object{}
	for i := 0; i < 148; i++ {
		code := string([]byte{'A' + byte(i/26), 'A' + byte(i%26), 'Z'})
		currencies[code] = 2
		labels[code] = object{"displayName": code, "symbol": code, "displayName-count-one": code}
	}
	sourceData := map[string]object{
		"currencyData":  {"supplemental": object{"currencyData": object{"fractions": object{"DEFAULT": object{"_digits": "2"}}}}},
		"en":            {"main": object{"en": object{"numbers": object{"currencies": labels}}}},
		"aliases":       {"supplemental": object{"metadata": object{"alias": object{"languageAlias": object{"iw": object{"_replacement": "he"}}, "territoryAlias": object{"AN": object{"_replacement": "CW SX"}}}}}},
		"likelySubtags": {"supplemental": object{"likelySubtags": object{"en": "en-Latn-US"}}},
		"parentLocales": {"supplemental": object{"parentLocales": object{"parentLocale": object{"en-GB": "en-001"}}}},
	}
	for name, v := range sourceData {
		b, e := encode(v)
		if e != nil {
			t.Fatal(e)
		}
		name += ".json"
		putTest(t, filepath.Join(root, "reference/cldr-48", name), b)
		sources[name] = object{"sha256": digest(b), "url": "https://example.test/" + name}
	}
	license := []byte("Unicode license fixture\n")
	putTest(t, filepath.Join(root, "reference/cldr-48/UNICODE-LICENSE.txt"), license)
	sources["UNICODE-LICENSE.txt"] = object{"sha256": digest(license)}
	p := object{"cldr_version": "48.0.0", "currencies": currencies, "locales": object{"en": "en"}, "default_locale": "en", "legacy_locale_aliases": object{}, "sources": sources}
	b, e := encode(p)
	if e != nil {
		t.Fatal(e)
	}
	putTest(t, filepath.Join(root, "reference/currency-policy.json"), b)
	putTest(t, filepath.Join(root, "internal/apicontract/v1/openapi.json"), []byte(`{"info":{"version":"2026-09-14"}}`))
	return root
}
func putTest(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestGenerateExportAndCheck(t *testing.T) {
	root := backendFixture(t)
	app := t.TempDir()
	putTest(t, filepath.Join(app, "lib/core/network/ledger_transport.dart"), []byte("static const version = '2026-09-14';"))
	if e := Run(root, []string{"--app", app}); e != nil {
		t.Fatal(e)
	}
	if e := Run(root, []string{"--check", "--app", app}); e != nil {
		t.Fatal(e)
	}
	if e := Run(app, []string{"--app-check"}); e != nil {
		t.Fatal(e)
	}
	original, e := os.ReadFile(filepath.Join(root, "reference/currencies/manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = Run(root, nil); e != nil {
		t.Fatal(e)
	}
	again, e := os.ReadFile(filepath.Join(root, "reference/currencies/manifest.json"))
	if e != nil || !bytes.Equal(original, again) {
		t.Fatal("nondeterministic output")
	}
	putTest(t, filepath.Join(app, "assets/reference/currencies/locales/en.json"), []byte("tampered"))
	if e = Run(app, []string{"--app-check"}); e == nil {
		t.Fatal("accepted asset tampering")
	}
	putTest(t, filepath.Join(root, "reference/cldr-48/en.json"), []byte("tampered"))
	if e = Run(root, []string{"--check"}); e == nil || !strings.Contains(e.Error(), "CLDR source changed") {
		t.Fatalf("expected pinned source rejection, got %v", e)
	}
}
func TestAppContractAndTraversal(t *testing.T) {
	for _, kind := range []string{"revision", "marker", "extra", "path"} {
		t.Run(kind, func(t *testing.T) {
			root := backendFixture(t)
			app := t.TempDir()
			putTest(t, filepath.Join(app, "lib/core/network/ledger_transport.dart"), []byte("static const version = '2026-09-14';"))
			if e := Run(root, []string{"--app", app}); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "revision":
				putTest(t, filepath.Join(app, "lib/core/network/ledger_transport.dart"), []byte("static const version = '2026-09-15';"))
			case "marker":
				putTest(t, filepath.Join(app, "assets/reference/contract.json"), []byte(`{}`))
			case "extra":
				putTest(t, filepath.Join(app, "assets/reference/currencies/extra"), nil)
			case "path":
				path := filepath.Join(app, "assets/reference/currencies/manifest.json")
				var manifest object
				if e := readJSON(path, &manifest); e != nil {
					t.Fatal(e)
				}
				hashes := object{"../outside": "bad"}
				manifest["files"] = hashes
				b, e := encode(hashes)
				if e != nil {
					t.Fatal(e)
				}
				manifest["content_sha256"] = digest(b)
				b, e = json.Marshal(manifest)
				if e != nil {
					t.Fatal(e)
				}
				putTest(t, path, b)
			}
			if e := Run(app, []string{"--app-check"}); e == nil {
				t.Fatal("accepted invalid application")
			}
		})
	}
}

func changeJSON(t *testing.T, path string, change func(object)) {
	t.Helper()
	var v object
	if e := readJSON(path, &v); e != nil {
		t.Fatal(e)
	}
	change(v)
	b, e := encode(v)
	if e != nil {
		t.Fatal(e)
	}
	putTest(t, path, b)
}
func changeSource(t *testing.T, root, name string, change func(object)) {
	t.Helper()
	path := filepath.Join(root, "reference/cldr-48", name+".json")
	changeJSON(t, path, change)
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	changeJSON(t, filepath.Join(root, "reference/currency-policy.json"), func(p object) { at(p, "sources", name+".json")["sha256"] = digest(b) })
}
func TestInvalidBackendInputs(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"policy-json": func(t *testing.T, r string) {
			putTest(t, filepath.Join(r, "reference/currency-policy.json"), []byte("{"))
		},
		"policy-schema": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { p["cldr_version"] = "49" })
		},
		"currency-code": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { m := at(p, "currencies"); delete(m, "AAZ"); m["bad"] = 2 })
		},
		"precision": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { at(p, "currencies")["AAZ"] = 3 })
		},
		"unpinned": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { delete(at(p, "sources"), "currencyData.json") })
		},
		"source-missing": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "reference/cldr-48/en.json")); e != nil {
				t.Fatal(e)
			}
		},
		"source-path": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { at(p, "sources")["../bad"] = object{"sha256": "bad"} })
		},
		"empty-label": func(t *testing.T, r string) {
			changeSource(t, r, "en", func(v object) { at(v, "main", "en", "numbers", "currencies", "AAZ")["displayName"] = "" })
		},
		"plural": func(t *testing.T, r string) {
			changeSource(t, r, "en", func(v object) {
				at(v, "main", "en", "numbers", "currencies", "AAZ")["displayName-count-invalid"] = "bad"
			})
		},
		"locale-path": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "reference/currency-policy.json"), func(p object) { p["locales"] = object{"../../bad": "en"} })
		},
		"language-alias": func(t *testing.T, r string) {
			changeSource(t, r, "aliases", func(v object) { at(v, "supplemental", "metadata", "alias", "languageAlias")["iw"] = false })
		},
		"territory-alias": func(t *testing.T, r string) {
			changeSource(t, r, "aliases", func(v object) { at(v, "supplemental", "metadata", "alias", "territoryAlias")["AN"] = false })
		},
		"empty-territory": func(t *testing.T, r string) {
			changeSource(t, r, "aliases", func(v object) {
				at(v, "supplemental", "metadata", "alias", "territoryAlias", "AN")["_replacement"] = ""
			})
		},
		"contract-missing": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "internal/apicontract/v1/openapi.json")); e != nil {
				t.Fatal(e)
			}
		},
		"contract-empty": func(t *testing.T, r string) {
			putTest(t, filepath.Join(r, "internal/apicontract/v1/openapi.json"), []byte(`{}`))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := backendFixture(t)
			change(t, r)
			if e := Run(r, []string{"--app", t.TempDir()}); e == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}
func refreshAppHash(t *testing.T, r, name string) {
	t.Helper()
	path := filepath.Join(r, "assets/reference/currencies", name)
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) {
		files := at(v, "files")
		files[name] = digest(b)
		encoded, e := encode(files)
		if e != nil {
			t.Fatal(e)
		}
		v["content_sha256"] = digest(encoded)
	})
}
func TestAppMalformedInputs(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"manifest-missing": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "assets/reference/currencies/manifest.json")); e != nil {
				t.Fatal(e)
			}
		},
		"manifest-schema": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) { v["schema_version"] = 9 })
		},
		"manifest-hash": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) { v["content_sha256"] = "bad" })
		},
		"unhashed-catalog": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) { v["catalog"] = "other.json" })
		},
		"unhashed-rules": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) { v["rules"] = "other.json" })
		},
		"unhashed-locale": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/manifest.json"), func(v object) { at(v, "locales", "en")["path"] = "other.json" })
		},
		"missing-asset": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "assets/reference/currencies/catalog.json")); e != nil {
				t.Fatal(e)
			}
		},
		"catalog-count": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/catalog.json"), func(v object) { delete(at(v, "currencies"), "AAZ") })
			refreshAppHash(t, r, "catalog.json")
		},
		"locale-name": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/locales/en.json"), func(v object) { v["locale"] = "wrong" })
			refreshAppHash(t, r, "locales/en.json")
		},
		"locale-label": func(t *testing.T, r string) {
			changeJSON(t, filepath.Join(r, "assets/reference/currencies/locales/en.json"), func(v object) { at(v, "currencies", "AAZ")["symbol"] = "" })
			refreshAppHash(t, r, "locales/en.json")
		},
		"retired-catalog": func(t *testing.T, r string) { putTest(t, filepath.Join(r, "assets/reference/currencies.json"), nil) },
		"missing-contract": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "assets/reference/contract.json")); e != nil {
				t.Fatal(e)
			}
		},
		"missing-transport": func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "lib/core/network/ledger_transport.dart")); e != nil {
				t.Fatal(e)
			}
		},
		"transport-shape": func(t *testing.T, r string) {
			putTest(t, filepath.Join(r, "lib/core/network/ledger_transport.dart"), []byte("unsupported"))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			root := backendFixture(t)
			app := t.TempDir()
			putTest(t, filepath.Join(app, "lib/core/network/ledger_transport.dart"), []byte("static const version = '2026-09-14';"))
			if e := Run(root, []string{"--app", app}); e != nil {
				t.Fatal(e)
			}
			change(t, app)
			if e := Run(app, []string{"--app-check"}); e == nil {
				t.Fatal("accepted invalid pack")
			}
		})
	}
}
func TestErrors(t *testing.T) {
	if _, e := encode(make(chan int)); e == nil {
		t.Fatal("accepted unsupported JSON")
	}
	r := t.TempDir()
	if e := Run(r, nil); e == nil {
		t.Fatal("accepted missing policy")
	}
	if e := writeOrCheck(filepath.Join(r, "missing"), nil, true); e == nil {
		t.Fatal("accepted missing output")
	}
	putTest(t, filepath.Join(r, "file"), nil)
	if e := writeOrCheck(filepath.Join(r, "file/child"), nil, false); e == nil {
		t.Fatal("accepted non-directory parent")
	}
}

func TestGeneratedMetadataCannotFollowSymlink(t *testing.T) {
	for _, appMarker := range []bool{false, true} {
		t.Run(fmt.Sprint(appMarker), func(t *testing.T) {
			root := backendFixture(t)
			app := t.TempDir()
			targetRoot := root
			relative := "internal/money/currencies.json"
			if appMarker {
				targetRoot = app
				relative = "assets/reference/contract.json"
			}
			outside := filepath.Join(t.TempDir(), "outside")
			putTest(t, outside, []byte("preserved"))
			target := filepath.Join(targetRoot, relative)
			if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(outside, target); e != nil {
				t.Skipf("symlinks unavailable: %v", e)
			}
			if e := Run(root, []string{"--app", app}); e == nil {
				t.Fatal("generated metadata followed symlink")
			}
			b, e := os.ReadFile(outside)
			if e != nil || string(b) != "preserved" {
				t.Fatalf("external file changed: %q %v", b, e)
			}
		})
	}
}

// Move a valid subtree outside the project and replace it with a symlink. The
// old files remain valid, so rejection must come from containment, not bad JSON.
func externalize(t *testing.T, root, relative string) string {
	t.Helper()
	source := filepath.Join(root, relative)
	outside := filepath.Join(t.TempDir(), "external")
	if e := os.Rename(source, outside); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, source); e != nil {
		t.Skipf("symlinks unavailable: %v", e)
	}
	return outside
}
func TestBackendProjectAncestorSymlinks(t *testing.T) {
	for _, relative := range []string{"reference", "reference/cldr-48", "reference/currency-policy.json", "reference/cldr-48/UNICODE-LICENSE.txt", "reference/currencies"} {
		t.Run(relative, func(t *testing.T) {
			root := backendFixture(t)
			if e := Run(root, nil); e != nil {
				t.Fatal(e)
			}
			externalize(t, root, relative)
			if e := Run(root, []string{"--check"}); e == nil {
				t.Fatal("checked data through project ancestor symlink")
			}
			if e := Run(root, nil); e == nil {
				t.Fatal("generated through project ancestor symlink")
			}
		})
	}
}
func TestAppProjectAncestorSymlinks(t *testing.T) {
	for _, relative := range []string{"assets", "assets/reference", "assets/reference/currencies", "assets/reference/currencies/manifest.json", "assets/reference/contract.json", "lib", "lib/core/network/ledger_transport.dart"} {
		t.Run(relative, func(t *testing.T) {
			root := backendFixture(t)
			app := t.TempDir()
			putTest(t, filepath.Join(app, "lib/core/network/ledger_transport.dart"), []byte("static const version = '2026-09-14';"))
			if e := Run(root, []string{"--app", app}); e != nil {
				t.Fatal(e)
			}
			externalize(t, app, relative)
			if e := Run(app, []string{"--app-check"}); e == nil {
				t.Fatal("app verifier accepted project ancestor symlink")
			}
			if strings.HasPrefix(relative, "assets") {
				if e := Run(root, []string{"--app", app}); e == nil {
					t.Fatal("exporter followed project ancestor symlink")
				}
			}
		})
	}
}
func TestAppExportDoesNotWriteThroughAbsentPackAncestor(t *testing.T) {
	root := backendFixture(t)
	app := t.TempDir()
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(app, "assets")); e != nil {
		t.Skipf("symlinks unavailable: %v", e)
	}
	if e := Run(root, []string{"--app", app}); e == nil {
		t.Fatal("exporter followed ancestor to a missing pack")
	}
	entries, e := os.ReadDir(outside)
	if e != nil || len(entries) != 0 {
		t.Fatalf("wrote outside declared app root: %v %v", entries, e)
	}
}
func TestBackendContractAncestorSymlink(t *testing.T) {
	root := backendFixture(t)
	externalize(t, root, "internal/apicontract")
	if e := Run(root, []string{"--app", t.TempDir()}); e == nil {
		t.Fatal("backend contract read followed ancestor symlink")
	}
}
