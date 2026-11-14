package android

import (
	"strings"
	"testing"

	"github.com/DIYgod/pinky/internal/screen"
)

// Synthetic current metadata; never commit a person's input-method dump.
const searchInputFixture = `Current Input Method Manager state:
  current=true
Input method client state for android.view.inputmethod.InputMethodManager@abc:
  mActive=true mRestartOnNextWindowFocus=false
  mServedConnecting=false
  mCurrentEditorInfo:
    inputType=0x2c0a1 imeOptions=0x12000003 privateImeOptions=null
    actionLabel=null actionId=0
    hintText=null label=null
    packageName=org.example.reader autofillId=123 fieldId=-1 fieldName=null
  mServedInputConnection=RemoteInputConnectionImpl{connection=InputConnection@abc mDeactivateRequested=false mServedView=WebView@abc}
Input method service state for org.example.ime:
  mInputStarted=true mInputViewStarted=false
  mInputEditorInfo:
    inputType=0x2c0a1 imeOptions=0x12000003 privateImeOptions=null
    actionLabel=null actionId=0
    hintText=null label=null
    packageName=org.example.reader autofillId=123 fieldId=-1 fieldName=null
  mUserDataRepository=
    mUserId=0
      unlocked=true
      hasMainConnection=true
      boundToMethod=true
      curClient=ClientState{abc mUid=10042 mPid=1234 mSelfReportedDisplayId=0}
      curEditorInfo:
        inputType=0x2c0a1 imeOptions=0x12000003 privateImeOptions=null
        actionLabel=null actionId=0
        hintText=null label=null
        packageName=org.example.reader autofillId=123 fieldId=-1 fieldName=null
      imeBindingState:
        mFocusedWindow()=android.os.BinderProxy@abc
        mFocusedWindowClient=ClientState{abc mUid=10042 mPid=1234 mSelfReportedDisplayId=0}
  ClientStates:
    old=true
`

func TestSearchInputMetadata(t *testing.T) {
	p := parseWindowIdentity(wmIdentityFixture, 1080, 1920)
	id := parseSearchInput(searchInputFixture, p)
	if id == "" {
		t.Fatal("no current search input proof")
	}
	other := *p
	other.token += "changed"
	if got := parseSearchInput(searchInputFixture, &other); got == "" || got == id {
		t.Fatal("proof not bound to foreground window")
	}
	if got := parseSearchInput(strings.ReplaceAll(searchInputFixture, "autofillId=123", "autofillId=124"), p); got == "" || got == id {
		t.Fatal("proof not bound to editor identity")
	}
	if parseSearchInput(searchInputFixture, nil) != "" {
		t.Fatal("accepted missing window identity")
	}
}

func TestSearchInputRejectsUnsafeMetadata(t *testing.T) {
	p := parseWindowIdentity(wmIdentityFixture, 1080, 1920)
	for _, tc := range []struct{ name, old, next string }{
		{"unknown format", "mUserDataRepository=", "unknownRepository="},
		{"duplicate repository", "  ClientStates:", "  mUserDataRepository=\n  ClientStates:"},
		{"multiple users", "    mUserId=0", "    mUserId=10\n    mUserId=0"},
		{"locked", "unlocked=true", "unlocked=false"},
		{"contradictory lock", "unlocked=true", "unlocked=true unlocked=false"},
		{"disconnected", "hasMainConnection=true", "hasMainConnection=false"},
		{"unbound", "boundToMethod=true", "boundToMethod=false"},
		{"foreign uid", "mUid=10042", "mUid=10043"},
		{"wrong display", "mSelfReportedDisplayId=0", "mSelfReportedDisplayId=7"},
		{"foreign package", "packageName=org.example.reader", "packageName=org.example.chat"},
		{"focus unknown", "mFocusedWindowClient=ClientState{abc mUid=10042 mPid=1234 mSelfReportedDisplayId=0}", "mFocusedWindowClient=null"},
		{"focus different client", "mFocusedWindowClient=ClientState{abc", "mFocusedWindowClient=ClientState{def"},
		{"null window", "mFocusedWindow()=android.os.BinderProxy@abc", "mFocusedWindow()=null"},
		{"chat send", "imeOptions=0x12000003", "imeOptions=0x12000004"},
		{"unknown action", "imeOptions=0x12000003", "imeOptions=0x12000000"},
		{"no enter action", "imeOptions=0x12000003", "imeOptions=0x52000003"},
		{"web password", "inputType=0x2c0a1", "inputType=0x2c0e1"},
		{"password", "inputType=0x2c0a1", "inputType=0x81"},
		{"visible password", "inputType=0x2c0a1", "inputType=0x91"},
		{"numeric code", "inputType=0x2c0a1", "inputType=0x12"},
		{"credential hint", "hintText=null label=null", "hintText=password label=null"},
		{"custom action", "actionLabel=null actionId=0", "actionLabel=Send actionId=42"},
		{"inactive client", "mActive=true", "mActive=false"},
		{"connecting client", "mServedConnecting=false", "mServedConnecting=true"},
		{"dead connection", "mDeactivateRequested=false", "mDeactivateRequested=true"},
		{"no served view", "mServedView=WebView@abc", "mServedView=null"},
		{"input stopped", "mInputStarted=true", "mInputStarted=false"},
		{"bad attrs", "inputType=0x2c0a1", "inputType=unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := strings.ReplaceAll(searchInputFixture, tc.old, tc.next)
			if s == searchInputFixture {
				t.Fatal("fixture did not change")
			}
			if parseSearchInput(s, p) != "" {
				t.Fatal("unsafe editor accepted")
			}
		})
	}
	// Current client disagrees with server/IME; matching stale history is irrelevant.
	s := strings.Replace(searchInputFixture, "autofillId=123", "autofillId=124", 1)
	if parseSearchInput(s, p) != "" {
		t.Fatal("conflicting current editor accepted")
	}
	if parseSearchInput("History:\n"+strings.ReplaceAll(searchInputFixture, "mUserDataRepository=", "oldRepository="), p) != "" {
		t.Fatal("history accepted as current state")
	}
}

func TestSearchInputNeedsVisibleSearchAndNoConflictingField(t *testing.T) {
	caption := screen.Element{Role: screen.RoleText, Label: "搜索", Bounds: screen.Rect{X: 800, Y: 200, W: 200, H: 80}}
	s := &screen.Screen{Width: 1080, Height: 1920, Elements: []screen.Element{caption}}
	if !searchCaption(s) {
		t.Fatal("missing visible search action")
	}
	s.Elements = append(s.Elements, screen.Element{Role: screen.RoleInput, Label: "chat message"})
	if searchCaption(s) {
		t.Fatal("chat composer with search header accepted")
	}
	s.Elements = []screen.Element{caption}
	s.Elements[0].Bounds.Y = 1000
	if searchCaption(s) {
		t.Fatal("search word in body accepted")
	}
}
