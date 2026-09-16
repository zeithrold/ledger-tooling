package governance

import (
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

type UIManifest struct {
	SchemaVersion int       `json:"schema_version"`
	Base          string    `json:"base"`
	Head          string    `json:"head"`
	Fingerprint   string    `json:"fingerprint"`
	Captures      []Capture `json:"captures"`
}
type Capture struct {
	Scenario         string  `json:"scenario"`
	Device           string  `json:"device"`
	Runtime          string  `json:"runtime"`
	Locale           string  `json:"locale"`
	Theme            string  `json:"theme"`
	TextScale        float64 `json:"text_scale"`
	Before           string  `json:"before"`
	After            string  `json:"after"`
	Diff             string  `json:"diff,omitempty"`
	ChangedPixels    int     `json:"changed_pixels"`
	AssertionsPassed bool    `json:"assertions_passed"`
}

func readPNG(root, path string) (image.Image, error) {
	p, e := safePath(root, path)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return png.Decode(f)
}
func UIReport(root, path string) error {
	p, e := safePath(root, path)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	var m UIManifest
	if e = json.Unmarshal(b, &m); e != nil {
		return e
	}
	fp, e := Fingerprint(root)
	if e != nil {
		return e
	}
	if m.SchemaVersion != 1 || m.Base == "" || m.Head == "" || m.Fingerprint != fp || len(m.Captures) == 0 {
		return fmt.Errorf("invalid or stale UI manifest")
	}
	dir, e := safePath(root, "build/ui-report")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	for i, c := range m.Captures {
		c.ChangedPixels = 0
		if c.Scenario == "" || c.Device == "" || c.Runtime == "" || c.Locale == "" || c.Theme == "" || c.TextScale <= 0 || !c.AssertionsPassed {
			return fmt.Errorf("capture lacks environment or passing behavior assertions")
		}
		before, e := readPNG(root, c.Before)
		if e != nil {
			return fmt.Errorf("before capture: %w", e)
		}
		after, e := readPNG(root, c.After)
		if e != nil {
			return fmt.Errorf("after capture: %w", e)
		}
		if before.Bounds() != after.Bounds() {
			return fmt.Errorf("cannot compare different image geometry")
		}
		diff := image.NewNRGBA(before.Bounds())
		for y := diff.Bounds().Min.Y; y < diff.Bounds().Max.Y; y++ {
			for x := diff.Bounds().Min.X; x < diff.Bounds().Max.X; x++ {
				ar, ag, ab, aa := before.At(x, y).RGBA()
				br, bg, bb, ba := after.At(x, y).RGBA()
				if ar != br || ag != bg || ab != bb || aa != ba {
					c.ChangedPixels++
					diff.Set(x, y, color.NRGBA{255, 0, 80, 255})
				} else {
					diff.Set(x, y, color.NRGBA{uint8(br >> 8), uint8(bg >> 8), uint8(bb >> 8), 90})
				}
			}
		}
		for name, img := range map[string]image.Image{"before": before, "after": after, "diff": diff} {
			file, e := os.Create(filepath.Join(dir, fmt.Sprintf("%d-%s.png", i, name)))
			if e != nil {
				return e
			}
			e = png.Encode(file, img)
			closeErr := file.Close()
			if e != nil {
				return e
			}
			if closeErr != nil {
				return closeErr
			}
		}
		c.Before = fmt.Sprintf("%d-before.png", i)
		c.After = fmt.Sprintf("%d-after.png", i)
		c.Diff = fmt.Sprintf("%d-diff.png", i)
		m.Captures[i] = c
	}
	if e = WriteJSON(filepath.Join(dir, "manifest.json"), m); e != nil {
		return e
	}
	t := template.Must(template.New("ui").Parse(`<!doctype html><meta charset="utf-8"><title>Ledger UI changes</title><style>body{font:16px system-ui;margin:2rem}section{border-top:1px solid #ccc;padding:1rem 0}img{max-width:30%;vertical-align:top}code{overflow-wrap:anywhere}</style><h1>Ledger UI changes</h1><p><code>{{.Base}}</code> → <code>{{.Head}}</code></p>{{range .Captures}}<section><h2>{{.Scenario}}</h2><p>{{.Device}} / {{.Runtime}} / {{.Locale}} / {{.Theme}} / text {{.TextScale}} — {{.ChangedPixels}} changed pixels</p><img alt="Before" src="{{.Before}}"><img alt="After" src="{{.After}}"><img alt="Difference" src="{{.Diff}}"></section>{{end}}`))
	f, e := os.Create(filepath.Join(dir, "index.html"))
	if e != nil {
		return e
	}
	defer f.Close()
	return t.Execute(f, m)
}
