package currency

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

func verifyApp(project string) error {
	root, e := safePath(project, "assets/reference/currencies")
	if e != nil {
		return e
	}
	var manifest struct {
		Schema  int               `json:"schema_version"`
		CLDR    string            `json:"cldr_version"`
		Content string            `json:"content_sha256"`
		Files   map[string]string `json:"files"`
		Catalog string            `json:"catalog"`
		Rules   string            `json:"rules"`
		Locales map[string]struct {
			Path string `json:"path"`
		} `json:"locales"`
	}
	manifestPath, e := safePath(root, "manifest.json")
	if e != nil {
		return e
	}
	if e := readJSON(manifestPath, &manifest); e != nil {
		return e
	}
	if manifest.Schema != 1 || manifest.CLDR != "48.0.0" || len(manifest.Files) == 0 || len(manifest.Locales) == 0 {
		return errors.New("invalid currency manifest")
	}
	encoded, e := encode(manifest.Files)
	if e != nil {
		return e
	}
	if digest(encoded) != manifest.Content {
		return errors.New("currency pack content hash mismatch")
	}
	files := map[string][]byte{}
	for name, expected := range manifest.Files {
		if name == "manifest.json" {
			return errors.New("manifest cannot hash itself")
		}
		path, e := safePath(root, name)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if digest(b) != expected {
			return fmt.Errorf("currency file hash mismatch: %s", name)
		}
		files[name] = b
	}
	b, e := os.ReadFile(manifestPath)
	if e != nil {
		return e
	}
	files["manifest.json"] = b
	if e = synchronize(root, files, true); e != nil {
		return e
	}
	load := func(name string, v any) error {
		if _, ok := manifest.Files[name]; !ok {
			return fmt.Errorf("unhashed currency asset: %s", name)
		}
		path, e := safePath(root, name)
		if e != nil {
			return e
		}
		return readJSON(path, v)
	}
	var catalog struct {
		Currencies map[string]object `json:"currencies"`
	}
	if e = load(manifest.Catalog, &catalog); e != nil {
		return e
	}
	if len(catalog.Currencies) != 148 {
		return errors.New("expected 148 supported currencies")
	}
	var rules object
	if e = load(manifest.Rules, &rules); e != nil {
		return e
	}
	for locale, info := range manifest.Locales {
		var data struct {
			Locale     string `json:"locale"`
			Currencies map[string]struct {
				Display string `json:"display_name"`
				Symbol  string `json:"symbol"`
			} `json:"currencies"`
		}
		if e = load(info.Path, &data); e != nil {
			return e
		}
		if data.Locale != locale || len(data.Currencies) != len(catalog.Currencies) {
			return fmt.Errorf("locale catalog mismatch: %s", locale)
		}
		for code := range catalog.Currencies {
			v, ok := data.Currencies[code]
			if !ok || v.Display == "" || v.Symbol == "" {
				return fmt.Errorf("missing locale label: %s/%s", locale, code)
			}
		}
	}
	if _, e = os.Stat(filepath.Join(project, "assets/reference/currencies.json")); e == nil {
		return errors.New("remove the retired bilingual catalog")
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	var contract struct {
		Schema   int    `json:"schema_version"`
		Hash     string `json:"currency_pack_sha256"`
		CLDR     string `json:"cldr_version"`
		Revision string `json:"contract_revision"`
	}
	contractPath, e := safePath(project, "assets/reference/contract.json")
	if e != nil {
		return e
	}
	if e = readJSON(contractPath, &contract); e != nil {
		return e
	}
	if contract.Schema != 1 || contract.Hash != manifest.Content || contract.CLDR != manifest.CLDR {
		return errors.New("stale application contract record; re-export currency pack")
	}
	transportPath, e := safePath(project, "lib/core/network/ledger_transport.dart")
	if e != nil {
		return e
	}
	transport, e := os.ReadFile(transportPath)
	if e != nil {
		return e
	}
	matches := regexp.MustCompile(`static const version = '([0-9]{4}-[0-9]{2}-[0-9]{2})';`).FindSubmatch(transport)
	if len(matches) != 2 {
		return errors.New("transport API version constant moved or changed shape")
	}
	if string(matches[1]) != contract.Revision {
		return fmt.Errorf("transport revision %s differs from exported contract %s", matches[1], contract.Revision)
	}
	return nil
}
