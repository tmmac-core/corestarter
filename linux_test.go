//go:build linux

package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemTextsFromLocale(t *testing.T) {
	for _, c := range []struct{ lcAll, lcMsg, lang, want string }{
		{"", "", "de_DE.UTF-8", "de"},
		{"", "ja_JP.UTF-8", "de_DE.UTF-8", "ja"},
		{"C", "", "de_DE.UTF-8", "en"},
		{"", "", "nl_BE@euro", "nl"},
		{"", "", "ro_RO", "ro"},
		{"", "", "fr_FR.UTF-8", "en"},
		{"", "", "", "en"},
	} {
		t.Setenv("LC_ALL", c.lcAll)
		t.Setenv("LC_MESSAGES", c.lcMsg)
		t.Setenv("LANG", c.lang)
		if got := systemTexts().lang; got != c.want {
			t.Errorf("LC_ALL=%q LC_MESSAGES=%q LANG=%q: %s, want %s", c.lcAll, c.lcMsg, c.lang, got, c.want)
		}
	}
}

func TestLinuxTextsPatched(t *testing.T) {
	for lang, tx := range textsByLang {
		if !strings.Contains(tx.noAdmin, "sudo") || strings.Contains(tx.crashHint, "Rechtsklick") {
			t.Errorf("%s: Windows wording left: %q / %q", lang, tx.noAdmin, tx.crashHint)
		}
	}
}

func TestProcStartTime(t *testing.T) {
	st, err := procStartTime(os.Getpid())
	if err != nil || st == 0 {
		t.Fatalf("procStartTime = %d, %v", st, err)
	}
	if !sameProcess(os.Getpid(), st) || sameProcess(os.Getpid(), st+1) {
		t.Fatal("sameProcess does not compare the start time")
	}
}

func TestDesktopQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/lib/core-starter/core-starter": "/usr/lib/core-starter/core-starter",
		"/opt/my apps/core-starter":          `"/opt/my apps/core-starter"`,
		`/x/a"b$c`:                           `"/x/a\\"b\\$c"`,
		"/opt/core%starter/core-starter":     "/opt/core%%starter/core-starter",
		"/opt/a&b/core-starter":              `"/opt/a&b/core-starter"`,
		`/opt/a b\c`:                         `"/opt/a b\\\\c"`,
	} {
		if got := desktopQuote(in); got != want {
			t.Errorf("desktopQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestTrayIconPNG(t *testing.T) {
	for look, b := range trayIcons {
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("look %d: %v", look, err)
		}
		if w := img.Bounds().Dx(); w < 64 {
			t.Errorf("look %d icon only %d px wide", look, w)
		}
	}
}

func TestXDGPaths(t *testing.T) {
	home := t.TempDir()
	if got := xdgDir("XDG_TEST_UNSET", home, ".config"); got != filepath.Join(home, ".config") {
		t.Errorf("default: %s", got)
	}
	t.Setenv("XDG_TEST_REL", "relative/dir") // the spec says: ignore
	if got := xdgDir("XDG_TEST_REL", home, ".config"); got != filepath.Join(home, ".config") {
		t.Errorf("relative value used: %s", got)
	}
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	dir, err := privateRunDir()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("run dir %s: %v %v", dir, fi.Mode(), err)
	}
	// A folder someone else prepared with loose permissions is refused.
	os.Chmod(dir, 0o755)
	if _, err := privateRunDir(); err == nil {
		t.Fatal("accepted a run folder with mode 0755")
	}
}

func TestChildEnvDropsOwnWebKitModes(t *testing.T) {
	t.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	t.Setenv("KEEP_ME", "1")
	if env := strings.Join(childEnv(), "\n"); !strings.Contains(env, "WEBKIT_DISABLE_DMABUF_RENDERER=1") {
		t.Fatal("a variable the user set was dropped")
	}
	t.Setenv(webkitStageEnv, "1")
	env := strings.Join(childEnv(), "\n")
	if strings.Contains(env, "WEBKIT_DISABLE_DMABUF_RENDERER") || strings.Contains(env, webkitStageEnv) || !strings.Contains(env, "KEEP_ME=1") {
		t.Fatalf("child environment: %s", env)
	}
}

func TestGodebugAppends(t *testing.T) {
	t.Setenv("GODEBUG", "")
	if g := godebugNetdns(); g != "GODEBUG=netdns=go" {
		t.Fatal(g)
	}
	t.Setenv("GODEBUG", "http2debug=1")
	if g := godebugNetdns(); g != "GODEBUG=http2debug=1,netdns=go" {
		t.Fatal(g)
	}
}

func TestLinuxFailureTexts(t *testing.T) {
	for lang, tx := range textsByLang {
		x := linuxFailures[lang]
		if s := tx.minerFailed("av"); s != x.minerPrefix+x.minerMissing {
			t.Errorf("%s av: %s", lang, s)
		}
		if s := tx.minerFailed("job"); s != x.minerPrefix+x.minerGuard {
			t.Errorf("%s job: %s", lang, s)
		}
		if s := tx.windowFailed("/nonexistent/starter.log"); strings.Contains(s, "/nonexistent") {
			t.Errorf("%s: names a log that does not exist: %s", lang, s)
		}
		if noTrayHint[lang] == "" {
			t.Errorf("%s: no tray hint", lang)
		}
	}
}
