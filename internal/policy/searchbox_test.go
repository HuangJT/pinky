package policy

import (
	"testing"

	"github.com/DIYgod/pinky/internal/screen"
)

func TestReadOnlyDriverVerifiedSearchInput(t *testing.T) {
	app := screen.App{ID: "com.tencent.mm", Name: "WeChat"}
	base := screen.Screen{App: app, Width: 1080, Height: 1920, CaptureIdentity: "wm:0:proof", SearchInputIdentity: "ime-search:proof"}
	e := commitsEngine(t)
	for _, tc := range []struct {
		name   string
		change func(*screen.Screen)
		want   Decision
	}{
		{"headless search", func(s *screen.Screen) {}, Allow},
		{"no proof", func(s *screen.Screen) { s.SearchInputIdentity = "" }, Deny},
		{"no window", func(s *screen.Screen) { s.CaptureIdentity = "" }, Deny},
		{"locked", func(s *screen.Screen) { s.Locked = true }, Deny},
		{"unknown app", func(s *screen.Screen) { s.App.ID = "" }, Deny},
		{"chat field", func(s *screen.Screen) { s.Elements = []screen.Element{{Role: screen.RoleInput, Label: "Message"}} }, Deny},
		{"unknown focus", func(s *screen.Screen) { s.Elements = []screen.Element{{States: []string{screen.StateFocused}}} }, Deny},
		{"credential", func(s *screen.Screen) {
			s.Elements = []screen.Element{{Role: screen.RoleInput, Label: "Password", States: []string{screen.StatePassword}}}
		}, Deny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.change(&s)
			v := e.Evaluate(Input{Op: OpType, App: app, Text: "new query", Submit: true, Screen: &s, ReadOnly: true})
			if v.Decision != tc.want {
				t.Fatalf("got %+v, want %v", v, tc.want)
			}
		})
	}
	// An explicitly chosen ordinary field must not inherit a different search proof.
	if IsSearchBox(&screen.Element{Role: screen.RoleInput, Label: "Message"}, &base) {
		t.Fatal("explicit chat input inherited search proof")
	}
}

// Read-only mode types into an app's own search box that reads as no field,
// only its placeholder (Xianyu, GitHub issue #76), and still into no other
// field.
func TestReadOnlyImpliedSearchBox(t *testing.T) {
	xianyu := screen.App{Name: "闲鱼", ID: "com.taobao.fleamarket"}
	el := func(role, label string, y int) screen.Element {
		return screen.Element{Role: role, Label: label, Bounds: screen.Rect{X: 100, Y: y, W: 600, H: 80}}
	}
	search := func(els ...screen.Element) *screen.Screen {
		return &screen.Screen{App: xianyu, Width: 1170, Height: 2532, Keyboard: true, Elements: append([]screen.Element{
			el(screen.RoleButton, "返回", 150),
			el(screen.RoleText, "搜索宝贝/帖子/用户", 150),
			el(screen.RoleButton, "搜索", 150),
		}, els...)}
	}
	noKeyboard := search()
	noKeyboard.Keyboard = false
	buttonOnly := &screen.Screen{App: xianyu, Width: 1170, Height: 2532, Keyboard: true, Elements: []screen.Element{
		el(screen.RoleButton, "搜索", 150),
		el(screen.RoleText, "你好", 1500),
	}}
	low := &screen.Screen{App: xianyu, Width: 1170, Height: 2532, Keyboard: true, Elements: []screen.Element{
		el(screen.RoleText, "搜索聊天记录", 1500),
	}}
	e := commitsEngine(t)
	for _, tc := range []struct {
		name   string
		target *screen.Element
		s      *screen.Screen
		want   Decision
	}{
		{"placeholder at the top", nil, search(), Allow},
		{"placeholder as the target", &search().Elements[1], search(), Allow},
		{"no keyboard", nil, noKeyboard, Deny},
		{"another field on screen", nil, search(el(screen.RoleInput, "留言", 2000)), Deny},
		{"only a search button", nil, buttonOnly, Deny},
		{"search words low on the screen", nil, low, Deny},
		{"an ordinary field", &screen.Element{Role: screen.RoleInput, Label: "留言", States: []string{screen.StateFocused}}, search(), Deny},
		{"no screen", nil, nil, Deny},
	} {
		in := Input{Op: OpType, App: xianyu, Text: "奥特曼", Target: tc.target, Screen: tc.s, ReadOnly: true}
		v := e.Evaluate(in)
		if v.Decision != tc.want || (tc.want == Deny && v.Rule != RuleReadOnly) {
			t.Errorf("%s: %+v, want %v", tc.name, v, tc.want)
		}
	}
}
