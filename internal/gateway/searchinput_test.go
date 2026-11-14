package gateway

import (
	"context"
	"sync"
	"testing"

	"github.com/DIYgod/pinky/internal/demo"
	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/policy"
	"github.com/DIYgod/pinky/internal/screen"
)

type searchProofDevice struct {
	driver.Device
	got driver.TypeOptions
}

// Reproduce the real pre-action path: its first read has OCR off and
// therefore cannot contain a corroborated search proof.
type ocrSearchDevice struct {
	*demo.Device
	mu    sync.Mutex
	got   driver.TypeOptions
	typed bool
	prove bool
}

func (d *ocrSearchDevice) Observe(_ context.Context, opt driver.ObserveOptions) (*screen.Screen, error) {
	s := &screen.Screen{Width: 1080, Height: 1920, App: screen.App{ID: "com.tencent.mm", Name: "WeChat"}, CaptureIdentity: "wm:proof"}
	if opt.OCR {
		s.Elements = []screen.Element{{Role: screen.RoleText, Label: "搜索", Source: screen.SourceOCR, Bounds: screen.Rect{X: 800, Y: 200, W: 200, H: 80}}}
		if d.prove {
			s.SearchInputIdentity = "ime-search:proof"
		}
	}
	return s, nil
}

func (d *ocrSearchDevice) Type(_ context.Context, _ string, opt driver.TypeOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got, d.typed = opt, true
	return nil
}

func TestReadOnlySearchProofRefreshedBeforeType(t *testing.T) {
	for _, prove := range []bool{true, false} {
		name := "unverified"
		if prove {
			name = "verified"
		}
		t.Run(name, func(t *testing.T) {
			base := demo.NewDevice("pixel", "Pixel", driver.Android)
			d := &ocrSearchDevice{Device: base, prove: prove}
			g := newDevGateway(t, base, d)
			s, _ := connect(t, g, "Search only")
			if err := g.SetReadOnly("pixel", true); err != nil {
				t.Fatal(err)
			}
			res := s.Act(context.Background(), Call{}, Action{Tool: "type", Text: "new search", Submit: true})
			want := StatusBlocked
			if prove {
				want = StatusSettled
			}
			mustStatus(t, res, want)
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.typed != prove {
				t.Fatalf("typed=%v want %v", d.typed, prove)
			}
			if prove && (d.got.SearchInputIdentity != "ime-search:proof" || !d.got.Submit) {
				t.Fatalf("missing fresh driver proof: %+v", d.got)
			}
		})
	}
}

func (d *searchProofDevice) Type(_ context.Context, _ string, opt driver.TypeOptions) error {
	d.got = opt
	return nil
}

func TestTypeCarriesSearchProofOnlyForImplicitTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		screen *screen.Screen
		target *screen.Element
		want   string
	}{
		{"recovered search", &screen.Screen{SearchInputIdentity: "proof"}, nil, "proof"},
		{"no screen", nil, nil, ""},
		{"explicit field", &screen.Screen{SearchInputIdentity: "proof"}, &screen.Element{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &searchProofDevice{}
			g := &Gateway{}
			p := &prep{target: tc.target, input: policy.Input{Screen: tc.screen}}
			if err := g.perform(context.Background(), d, Action{Tool: "type", Text: "query", Submit: true}, p); err != nil {
				t.Fatal(err)
			}
			if d.got.SearchInputIdentity != tc.want || !d.got.Submit {
				t.Fatalf("wrong options: %+v", d.got)
			}
		})
	}
}
