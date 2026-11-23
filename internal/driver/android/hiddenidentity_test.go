package android

import (
	"context"
	"strings"
	"testing"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/adb"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
	"github.com/DIYgod/pinky/internal/driver/android/scrcpy"
	"github.com/DIYgod/pinky/internal/ocr"
	"github.com/DIYgod/pinky/internal/screen"
)

// A wholly absent main-display tree must still reach the strict window
// proof and current-editor checks, even when the upstream hidden-app
// fallback can name the focused activity. All inputs here are synthetic.
func TestHiddenEmptyTreeKeepsWindowAndSearchProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		dump string
		want bool
	}{
		{"verified search", wmIdentityFixture, true},
		{"protected window", strings.Replace(wmIdentityFixture, "fl=LAYOUT_IN_SCREEN", "fl=SECURE LAYOUT_IN_SCREEN", 1), false},
		{"focused overlay", strings.Replace(wmIdentityFixture, "ty=STATUS_BAR", "ty=SYSTEM_ALERT", 1), false},
		{"owner mismatch", strings.Replace(wmIdentityFixture, "package=org.example.reader", "package=org.example.other", 1), false},
		{"unknown window dump", "unsupported format", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubOCR(t, &ocrStub{lines: []ocr.Line{line("搜索", 900, 180), line("Search results", 300, 600)}})
			ts := newTreeServer(t)
			ts.set(0, &companion.Tree{Display: companion.Display{Width: 1080, Height: 1920, DensityDPI: 440}})
			ph := &hiddenPhone{focused: "Display: mDisplayId=0\n mFocusedApp=ActivityRecord{abc u0 org.example.reader/.WebView t4}\n"}
			f := startFakeADB(t)
			f.set(func(f *fakeADB) {
				f.props = map[string]string{"phone": pixelProps}
				f.run = func(id, svc string) (string, bool) {
					switch svc {
					case "shell:" + windowIdentityCommand:
						return tc.dump, true
					case "shell:" + searchInputCommand:
						return searchInputFixture, true
					}
					if strings.HasPrefix(svc, "exec:screencap -p") {
						return string(grayPNG(1080, 1920)), true
					}
					return ph.run(id, svc)
				}
			})
			cfg := Config{Client: &adb.Client{Addr: f.ln.Addr().String()}, Logf: t.Logf}
			cfg.defaults()
			d := newDevice(cfg, driver.Info{ID: "phone", Connection: "usb"}, deviceProps{SDK: 35})
			d.listApps = func(context.Context) ([]scrcpy.App, error) {
				return []scrcpy.App{{Package: "org.example.reader", Name: "Reader"}}, nil
			}
			if _, err := d.cachedApps(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			c := companion.New(forwardRunner{addr: ts.ln.Addr().String()})
			d.comp.client, d.comp.on, d.comp.live = c, true, true
			t.Cleanup(func() { c.Close() })
			s, _, err := d.ObserveWithText(context.Background(), driver.ObserveOptions{
				OCR: true, MayCapture: func(s *screen.Screen) bool { return s.App.ID != "" },
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want {
				if s.App.ID != "org.example.reader" || s.CaptureIdentity == "" || s.SearchInputIdentity == "" {
					t.Fatalf("lost strict window/search proof: app=%q capture=%q search=%q", s.App.ID, s.CaptureIdentity, s.SearchInputIdentity)
				}
			} else if s.App.ID != "" || s.CaptureIdentity != "" || s.SearchInputIdentity != "" || len(ocrLabels(s)) != 0 {
				t.Fatalf("focused activity bypassed an invalid window proof: app=%q capture=%q search=%q", s.App.ID, s.CaptureIdentity, s.SearchInputIdentity)
			}
		})
	}
}
