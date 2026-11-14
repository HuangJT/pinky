package android

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/screen"
)

// InputMethodManager exposes the connected editor even when a WebView
// exposes no accessibility root and a hardware/headless IME has no window.
// Read metadata only; never log the dump, which can contain input history.
const searchInputCommand = "dumpsys input_method"

var (
	inputClientRE = regexp.MustCompile(`ClientState\{([a-zA-Z0-9]+) mUid=(\d+) mPid=(\d+) mSelfReportedDisplayId=(\d+)\}`)
	inputAttrsRE  = regexp.MustCompile(`(?m)^\s*inputType=(0x[0-9a-fA-F]+) imeOptions=(0x[0-9a-fA-F]+) privateImeOptions=null\s*$`)
	inputPkgRE    = regexp.MustCompile(`(?m)^\s*packageName=([A-Za-z0-9_.]+) autofillId=([^\s]+) fieldId=(-?\d+) fieldName=null\s*$`)
)

func plainSearchLabels(editor string) bool {
	action, a := uniqueLineValue(editor, "actionLabel")
	hint, h := uniqueLineValue(editor, "hintText")
	return a && h && action == "null actionId=0" && hint == "null label=null"
}

// indentedBlock selects exactly one current block. Historical editor
// entries, duplicate keys and unsupported layouts cannot supply a proof.
func indentedBlock(out, header string) (string, bool) {
	lines := strings.Split(out, "\n")
	start, indent := -1, 0
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			if start >= 0 {
				return "", false
			}
			start, indent = i, len(line)-len(strings.TrimLeft(line, " "))
		}
	}
	if start < 0 {
		return "", false
	}
	end := start + 1
	for ; end < len(lines); end++ {
		if strings.TrimSpace(lines[end]) == "" {
			continue
		}
		if len(lines[end])-len(strings.TrimLeft(lines[end], " ")) <= indent {
			break
		}
	}
	return strings.Join(lines[start+1:end], "\n"), true
}

func uniqueLineValue(block, key string) (string, bool) {
	value, count := "", 0
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			value, count = strings.TrimPrefix(line, key+"="), count+1
		}
	}
	return value, count == 1
}

// parseSearchInput accepts the current Android 15 server/client/IME
// agreement, not a previously focused field in its history. Unsupported
// Android dump variants remain unknown. The window proof independently
// establishes the unlocked display, foreground owner and absence of overlays.
func parseSearchInput(out string, proof *windowIdentity) string {
	if proof == nil || proof.app == "" || proof.uid == "" || proof.token == "" {
		return ""
	}
	repo, ok := indentedBlock(out, "mUserDataRepository=")
	if !ok || strings.Count(repo, "mUserId=") != 1 {
		return ""
	}
	for _, k := range []string{"unlocked", "hasMainConnection", "boundToMethod"} {
		v, known := wmBool(repo, k)
		if !known || !v {
			return ""
		}
	}
	client, ok := uniqueLineValue(repo, "curClient")
	if !ok {
		return ""
	}
	m := inputClientRE.FindStringSubmatch(client)
	if m == nil || m[0] != client || m[2] != proof.uid || m[4] != "0" {
		return ""
	}
	focus, ok := uniqueLineValue(repo, "mFocusedWindowClient")
	if !ok || focus != client {
		return ""
	}
	window, ok := uniqueLineValue(repo, "mFocusedWindow()")
	if !ok || !strings.HasPrefix(window, "android.os.BinderProxy@") {
		return ""
	}
	current, ok := indentedBlock(repo, "curEditorInfo:")
	if !ok {
		return ""
	}
	attrs := inputAttrsRE.FindAllStringSubmatch(current, -1)
	pkgs := inputPkgRE.FindAllStringSubmatch(current, -1)
	if len(attrs) != 1 || len(pkgs) != 1 || pkgs[0][1] != proof.app {
		return ""
	}
	inputType, err := strconv.ParseUint(attrs[0][1], 0, 32)
	if err != nil || (inputType&0xfff != 0x1 && inputType&0xfff != 0xa1) {
		return "" // only ordinary text / web text; never password, number or phone
	}
	imeOptions, err := strconv.ParseUint(attrs[0][2], 0, 32)
	if err != nil || imeOptions&0xff != 3 || imeOptions&0x40000000 != 0 {
		return "" // IME_ACTION_SEARCH, with no IME_FLAG_NO_ENTER_ACTION
	}
	if !plainSearchLabels(current) {
		return "" // this fallback does not guess custom action/credential labels
	}
	// Independently corroborate the live app client and connected IME.
	clientStart := strings.Index(out, "\nInput method client state for ")
	imeStart := strings.Index(out, "\nInput method service state for ")
	if clientStart < 0 || imeStart <= clientStart || strings.Count(out, "\nInput method client state for ") != 1 || strings.Count(out, "\nInput method service state for ") != 1 {
		return ""
	}
	app := out[clientStart:imeStart]
	active, known := wmBool(app, "mActive")
	connecting, ck := wmBool(app, "mServedConnecting")
	if !known || !active || !ck || connecting {
		return ""
	}
	connection, ok := uniqueLineValue(app, "mServedInputConnection")
	deactivated, connectionKnown := wmBool(connection, "mDeactivateRequested")
	if !ok || !strings.HasPrefix(connection, "RemoteInputConnectionImpl{") || !connectionKnown || deactivated || !strings.Contains(connection, " mServedView=") || strings.Contains(connection, "mServedView=null") {
		return ""
	}
	appEditor, ok := indentedBlock(app, "mCurrentEditorInfo:")
	if !ok {
		return ""
	}
	ime := out[imeStart:]
	// Only the service's immediate header, before its historical sections.
	end := strings.Index(ime, "\n  mUserDataRepository=")
	if end >= 0 {
		ime = ime[:end]
	}
	started, known := wmBool(ime, "mInputStarted")
	if !known || !started {
		return ""
	}
	imeEditor, ok := indentedBlock(ime, "mInputEditorInfo:")
	if !ok {
		return ""
	}
	for _, other := range []string{appEditor, imeEditor} {
		a := inputAttrsRE.FindAllStringSubmatch(other, -1)
		p := inputPkgRE.FindAllStringSubmatch(other, -1)
		if !plainSearchLabels(other) || len(a) != 1 || len(p) != 1 || a[0][1] != attrs[0][1] || a[0][2] != attrs[0][2] || p[0][1] != pkgs[0][1] || p[0][2] != pkgs[0][2] || p[0][3] != pkgs[0][3] {
			return ""
		}
	}
	material := strings.Join([]string{proof.token, client, window, attrs[0][1], attrs[0][2], pkgs[0][1], pkgs[0][2], pkgs[0][3], connection}, "\n")
	return fmt.Sprintf("ime-search:%x", sha256.Sum256([]byte(material)))
}

// Require a visible search action at the top too. OCR by itself is never
// enough; it only corroborates the independently bound editor metadata.
func searchCaption(s *screen.Screen) bool {
	if s == nil || s.Locked || s.Width <= 0 || s.Height <= 0 {
		return false
	}
	found := false
	for _, e := range s.Elements {
		if e.Role == screen.RoleInput || e.Has(screen.StateEditable) || e.Has(screen.StateFocused) || e.Has(screen.StatePassword) {
			return false
		}
		label := strings.ToLower(strings.TrimSpace(e.Label))
		if !e.Bounds.Empty() && e.Bounds.Center().Y < s.Height/4 && (label == "搜索" || label == "search") {
			found = true
		}
	}
	return found
}

func (d *Device) readSearchInput(ctx context.Context, proof *windowIdentity) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := d.client.Shell(ctx, d.info.ID, searchInputCommand)
	if err != nil {
		return ""
	}
	return parseSearchInput(string(out), proof)
}

func (d *Device) recoverSearchInput(ctx context.Context, s *screen.Screen, proof *windowIdentity) {
	if proof == nil || s.CaptureIdentity != proof.token || !searchCaption(s) {
		return
	}
	token := d.readSearchInput(ctx, proof)
	if token == "" || d.proofMoved(ctx, s, proof) || token != d.readSearchInput(ctx, proof) {
		return
	}
	s.SearchInputIdentity = token
}

func (d *Device) checkSearchInput(ctx context.Context, expected string) error {
	w, h, err := d.naturalSize(ctx)
	if err != nil {
		return err
	}
	p := d.readWindowIdentity(ctx, w, h)
	if p == nil || d.readSearchInput(ctx, p) != expected {
		return fmt.Errorf("the verified search input changed or is unavailable")
	}
	return nil
}
