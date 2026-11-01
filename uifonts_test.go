package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestWithFonts(t *testing.T) {
	page, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), uiFontSlot) {
		t.Fatal("page has no font slot")
	}
	out := withFonts(string(page))
	if strings.Contains(out, uiFontSlot) {
		t.Error("slot not replaced")
	}
	if n := strings.Count(out, "@font-face {"); n != len(uiFontFaces) {
		t.Errorf("%d @font-face rules, want %d", n, len(uiFontFaces))
	}
	if !strings.Contains(out, "font-src data:") {
		t.Error("the page's CSP must allow data: fonts")
	}
	// NavigateToString takes at most 2 MB.
	if len(out) > 1<<20 {
		t.Errorf("page with fonts is %d bytes", len(out))
	}
}

// Scripts for WebKitGTK stay ASCII and mean the same (asciiJS).
func TestASCIIJS(t *testing.T) {
	// "~" stands for a backslash plus u in the expected scripts.
	for in, want := range map[string]string{
		`window.__reply(1,{"a":1})`: `window.__reply(1,{"a":1})`,
		`x("Grüße – „ok“")`:         `x("Gr~00fc~00dfe ~2013 ~201eok~201c")`,
		`"ログ 🐇"`:                    `"~30ed~30b0 ~d83d~dc07"`,
		`"/home/jörg/.local/share"`: `"/home/j~00f6rg/.local/share"`,
	} {
		want = strings.ReplaceAll(want, "~", `\`+"u")
		if got := asciiJS(in); got != want {
			t.Errorf("asciiJS(%q) = %q, want %q", in, got, want)
		}
	}
	// The whole init reply with NOTICE.md: ASCII only, same value for JSON.
	b, _ := json.Marshal(map[string]string{"notice": noticeText})
	out := asciiJS(string(b))
	for i := 0; i < len(out); i++ {
		if out[i] >= 0x80 {
			t.Fatalf("non-ASCII byte at %d", i)
		}
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(out), &back); err != nil || back["notice"] != noticeText {
		t.Errorf("escaped reply reads back differently: %v", err)
	}
}

// Long scripts reach WebKitGTK in pieces under 16 KB that rebuild the same
// script (splitScript); short ones stay as they are.
func TestSplitScript(t *testing.T) {
	for _, n := range []int{3, 2 * scriptPiece} {
		js := strings.Repeat("x", n)
		if got := splitScript(js); len(got) != 1 || got[0] != js {
			t.Errorf("script of %d bytes changed", n)
		}
	}
	if got := splitScript(strings.Repeat("x", 2*scriptPiece+1)); len(got) != 5 {
		t.Errorf("script of %d bytes: %d pieces, want 5", 2*scriptPiece+1, len(got))
	}
	// The pieces run as an inline script element: the page must allow those.
	page, _ := os.ReadFile("ui/index.html")
	if !strings.Contains(string(page), "script-src 'unsafe-inline'") {
		t.Error("splitScript needs script-src 'unsafe-inline' in the page's CSP")
	}
	b, _ := json.Marshal(map[string]any{"notice": noticeText, "icon": base64.StdEncoding.EncodeToString(uiIcon)})
	js := asciiJS("window.__reply(1," + string(b) + `); "\\ \" </script>"`)
	pieces := splitScript(js)
	var rebuilt strings.Builder
	for i, p := range pieces {
		if len(p) >= 16<<10 {
			t.Errorf("piece %d is %d bytes", i, len(p))
		}
		if arg, ok := strings.CutPrefix(p, "window.__cs.push("); ok {
			var s string
			if err := json.Unmarshal([]byte(strings.TrimSuffix(arg, ")")), &s); err != nil {
				t.Fatalf("piece %d: %v", i, err)
			}
			rebuilt.WriteString(s)
		}
	}
	if rebuilt.String() != js {
		t.Error("pieces do not rebuild the script")
	}
	if pieces[0] != "window.__cs = []" || !strings.Contains(pieces[len(pieces)-1], `s.textContent = window.__cs.join("")`) {
		t.Errorf("first or last piece is not the start or the run: %q, %q", pieces[0], pieces[len(pieces)-1])
	}
}
