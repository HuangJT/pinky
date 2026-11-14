package gateway

import (
	"context"
	"testing"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/policy"
	"github.com/DIYgod/pinky/internal/screen"
)

type searchProofDevice struct {
	driver.Device
	got driver.TypeOptions
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
