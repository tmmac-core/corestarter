package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// jsAgree: snippet for the window test scripts (inside an async function with
// wait()). The notes must lock the save button; a page that does not returns
// early, so the test times out. Then it ticks the box and waits until the
// button is free. Locked = data-blocked: the button stays clickable and leads
// to the tick box instead of saving.
const jsAgree = `{
			const save = document.getElementById("save"), card = document.getElementById("consentCard");
			const locked = () => save.disabled || save.dataset.blocked === "1";
			if (card.classList.contains("hidden") || !locked()) return;
			document.getElementById("agree").click();
			for (let i = 0; i < 50 && locked(); i++) await wait(100);
		}`

// agreedConsent: settings of a user who accepted the current notes.
func agreedConsent() Consent {
	var c Consent
	c.agree(true, true, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	return c
}

func TestConsentRules(t *testing.T) {
	s := DefaultSettings()
	if s.nodeConsented() || s.mineConsented() || s.consentProblem(nil) == nil {
		t.Fatal("fresh settings count as consented")
	}
	now := time.Date(2026, 10, 28, 15, 4, 5, 999, time.Local)
	s.Consent.agree(true, false, now)
	if !s.nodeConsented() || s.mineConsented() {
		t.Fatalf("after node agree: %+v", s.Consent)
	}
	if s.Consent.Node.Version != consentNodeVersion || !s.Consent.Node.At.Equal(now.Truncate(time.Second)) {
		t.Errorf("node mark %+v", s.Consent.Node)
	}
	if s.consentProblem(nil) != nil {
		t.Error("node only, mining off: should be fine")
	}
	s.Mine = true
	if s.consentProblem(nil) == nil {
		t.Error("mining without its consent accepted")
	}
	s.Consent.agree(false, true, now)
	if s.consentProblem(nil) != nil || !s.mineConsented() {
		t.Errorf("mining consent not taken: %+v", s.Consent)
	}
	// A newer text: the old mark no longer covers it.
	s.Consent.Mine.Version = consentMineVersion - 1
	if s.mineConsented() || s.consentProblem(nil) == nil {
		t.Error("an older text version still counts")
	}
	// A newer program's mark (downgrade) still counts.
	s.Consent.Mine.Version = consentMineVersion + 1
	if !s.mineConsented() {
		t.Error("a newer version's consent does not count")
	}
}

// Later saves only need the consent when they switch something on: an older
// installation without (current) consent can still switch mining off or change
// the power, but not switch node, mining or autostart on.
func TestConsentOnlyWhenSwitchingOn(t *testing.T) {
	prev := Settings{Node: true, Mine: true, Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Power: 50}
	s := prev
	s.Mine, s.Power = false, 75
	if err := s.consentProblem(&prev); err != nil {
		t.Errorf("switching mining off needs consent: %v", err)
	}
	s = prev
	s.Power = 25
	if err := s.consentProblem(&prev); err != nil {
		t.Errorf("changing the power needs consent: %v", err)
	}
	off := prev
	off.Mine = false
	if err := prev.consentProblem(&off); err == nil {
		t.Error("switching mining on without consent accepted")
	}
	s = prev
	s.Autostart = true
	if err := s.consentProblem(&prev); err == nil {
		t.Error("switching autostart on without consent accepted")
	}
	s.Consent = agreedConsent()
	if err := s.consentProblem(&prev); err != nil {
		t.Errorf("with consent: %v", err)
	}
}

// Switching autostart on later shows its note again: the save needs the tick
// (agree.node) even with a current node consent; other saves do not.
func TestConsentAutostartLater(t *testing.T) {
	prev := Settings{Node: true, Power: 50, Consent: agreedConsent()}
	s := prev
	s.Autostart = true
	if err := s.consentProblem(&prev); err != nil {
		t.Fatalf("node consent current: %v", err)
	}
	if err := s.autostartProblem(&prev, false); err != errConsent {
		t.Errorf("autostart switched on without the tick: %v", err)
	}
	if err := s.autostartProblem(&prev, true); err != nil {
		t.Errorf("autostart switched on with the tick: %v", err)
	}
	// The tick re-stamps the node consent (the save handler's agree call).
	old := s.Consent.Node.At
	later := old.Add(time.Hour)
	s.Consent.agree(true, false, later)
	if !s.Consent.Node.At.Equal(later) || s.Consent.Node.Version != consentNodeVersion {
		t.Errorf("node consent not re-stamped: %+v", s.Consent.Node)
	}
	// Autostart already on, switched off, or the first save: no extra tick.
	on := prev
	on.Autostart = true
	if err := on.autostartProblem(&on, false); err != nil {
		t.Errorf("autostart stays on: %v", err)
	}
	if err := prev.autostartProblem(&on, false); err != nil {
		t.Errorf("autostart switched off: %v", err)
	}
	if err := on.autostartProblem(nil, false); err != nil {
		t.Errorf("first save (consentProblem decides): %v", err)
	}
}

// The consent survives saving, loading and the window's other writes, and the
// tray's language switch keeps it.
func TestConsentSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.Consent = agreedConsent()
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"consent"`) || !strings.Contains(string(raw), fmt.Sprintf(`"version": %d`, consentNodeVersion)) {
		t.Errorf("settings.json without consent:\n%s", raw)
	}
	got, err := LoadSettings(path)
	if err != nil || !got.nodeConsented() || !got.mineConsented() || !got.Consent.Node.At.Equal(s.Consent.Node.At) {
		t.Fatalf("loaded %+v, %v", got.Consent, err)
	}
	if p := savedSettings(path); p == nil || !p.Consent.Node.covers(consentNodeVersion) {
		t.Errorf("savedSettings %+v", p)
	}
	if p := savedSettings(filepath.Join(t.TempDir(), "missing.json")); p != nil {
		t.Errorf("savedSettings without a file: %+v", p)
	}
}

// TestConsentVersion: the texts of each part (window and fallback dialog,
// every language) match the hash recorded for its current version. A changed
// text needs a raised version of its part, or nobody is asked again. The node
// part includes NOTICE.md (the licence and liability terms it refers to) and
// the shared lines (title, intro, tick box); the mining part its own lines
// plus the shared ones.
func TestConsentVersion(t *testing.T) {
	node, mine := consentHashes(t)
	for _, c := range []struct {
		part    string
		version int
		got     string
		want    map[int]string
	}{
		{"node", consentNodeVersion, node, map[int]string{1: "9a681d8979c7704370ea78cf78061aabd6bb52290edf797b646d45b0effdaf3d", 2: "a4ad0e337b89378cfe016c36ad4a371162cd0014f0aecb84b24e8f65361a71dc", 3: "5615d3820d1407105f7596e316497141403447f69d2539c88de1e8693678f00f", 4: "47e7fe46dc71de376f78cbf152ac7831a326caed904e3f1cccc2d4cad46c7e00"}},
		{"mine", consentMineVersion, mine, map[int]string{1: "e5dbadec5d78077162af0e818e189eb580c1f226853610daf8e86baf6c35ef1c", 2: "4a5c9284f3d72be7214f6f9feebcce1f2baf1e002927bb6cf84ba8f35fad0b8b", 3: "1e4d5059f148d3fa41ce1f165d5b75beb5134dafb2f6a45b4993646226f83888"}},
	} {
		switch w := c.want[c.version]; {
		case w == "":
			t.Errorf("%s: no hash recorded for version %d; record %q", c.part, c.version, c.got)
		case w != c.got:
			t.Errorf("%s consent texts changed (hash %s, recorded %s for version %d): raise consent%sVersion in consent.go and record the new hash", c.part, c.got, w, c.version, strings.ToUpper(c.part[:1])+c.part[1:])
		}
	}
}

// consentHashes splits the CONSENT block by key: lines of node* keys count for
// the node part only, mine* keys for mining only, all others for both.
func consentHashes(t *testing.T) (node, mine string) {
	t.Helper()
	hn, hm := sha256.New(), sha256.New()
	key := regexp.MustCompile(`^\s+([a-zA-Z]+):`)
	for _, line := range strings.Split(consentBlock(t), "\n") {
		k := ""
		if m := key.FindStringSubmatch(line); m != nil {
			k = m[1]
		}
		if !strings.HasPrefix(k, "mine") {
			hn.Write([]byte(line + "\n"))
		}
		if !strings.HasPrefix(k, "node") {
			hm.Write([]byte(line + "\n"))
		}
	}
	for _, lang := range uiLangs {
		hn.Write([]byte(textsByLang[lang].installBody))
	}
	hn.Write([]byte(noticeText))
	return hex.EncodeToString(hn.Sum(nil)), hex.EncodeToString(hm.Sum(nil))
}

func consentBlock(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a, b := strings.Index(s, "// consent:begin\n"), strings.Index(s, "// consent:end\n")
	if a < 0 || b < a {
		t.Fatal("consent markers missing in ui/index.html")
	}
	return s[a:b]
}

// TestConsentTextsComplete: every language of the window's CONSENT block has
// the German keys, none empty, and the fallback dialog asks for the "Yes".
func TestConsentTextsComplete(t *testing.T) {
	block := consentBlock(t)
	langRe := regexp.MustCompile(`(?m)^  ([a-z]{2}): \{$`)
	keyRe := regexp.MustCompile(`(?m)(?:^ {4}|, )([a-zA-Z]+): `)
	idx := langRe.FindAllStringSubmatchIndex(block, -1)
	keys := map[string][]string{}
	for i, m := range idx {
		end := len(block)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		part := block[m[1]:end]
		if strings.Contains(part, `: ""`) {
			t.Errorf("%s: empty text", block[m[2]:m[3]])
		}
		for _, k := range keyRe.FindAllStringSubmatch(part, -1) {
			keys[block[m[2]:m[3]]] = append(keys[block[m[2]:m[3]]], k[1])
		}
	}
	de := keys["de"]
	if len(de) < 10 {
		t.Fatalf("German consent keys not found: %v", de)
	}
	for _, lang := range uiLangs {
		if !slices.Equal(keys[lang], de) {
			t.Errorf("%s keys %v, German has %v", lang, keys[lang], de)
		}
	}
	if len(keys) != len(uiLangs) {
		t.Errorf("CONSENT has %d languages, uiLangs %d", len(keys), len(uiLangs))
	}
	for _, lang := range uiLangs {
		if !strings.Contains(textsByLang[lang].installBody, "Tell Me More About Core") {
			t.Errorf("%s: fallback dialog does not name the publisher", lang)
		}
	}
	if strings.Contains(noticeText, "## Additional terms") == false {
		t.Error("embedded NOTICE.md lacks the additional terms")
	}
}

// The forbidden words for the fixed support share and the publisher rule,
// checked where users read them: the window and the Go texts.
func TestConsentWording(t *testing.T) {
	raw, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	all := string(raw)
	for _, lang := range uiLangs {
		all += fmt.Sprintf("%#v", *textsByLang[lang])
	}
	for _, bad := range []string{"Spende", "spende", "donation", "Donation", "gemined", "passives Einkommen", "passive income"} {
		if strings.Contains(all, bad) {
			t.Errorf("user-facing text contains %q", bad)
		}
	}
}

// TestConsentKeysOwn: texts() lays CONSENT over L, so a shared key would let
// the consent card show a page text (or the page a consent text) unnoticed.
func TestConsentKeysOwn(t *testing.T) {
	raw, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a, b := strings.Index(s, "const L = {\n  de: {\n"), strings.Index(s, "\n  en: {\n")
	if a < 0 || b < a {
		t.Fatal("German block of L not found")
	}
	keyRe := regexp.MustCompile(`(?m)(?:^ {4}|, )([a-zA-Z]+): `)
	page := map[string]bool{}
	for _, k := range keyRe.FindAllStringSubmatch(s[a:b], -1) {
		page[k[1]] = true
	}
	block := consentBlock(t)
	de := block[strings.Index(block, "  de: {"):strings.Index(block, "  en: {")]
	for _, k := range keyRe.FindAllStringSubmatch(de, -1) {
		if page[k[1]] {
			t.Errorf("CONSENT key %q also exists in L", k[1])
		}
	}
}

// TestSiteURL: the window's links only ever lead to the publisher's own pages.
func TestSiteURL(t *testing.T) {
	for _, c := range []struct{ page, lang, want string }{
		{"site", "de", "https://tellmemoreaboutcore.com/de/"},
		{"site", "ja", "https://tellmemoreaboutcore.com/"},
		{"imprint", "de", "https://tellmemoreaboutcore.com/de/impressum/"},
		{"imprint", "nl", "https://tellmemoreaboutcore.com/imprint/"},
		{"feedback", "de", "https://tellmemoreaboutcore.com/de/impressum/#contact"},
		{"feedback", "en", "https://tellmemoreaboutcore.com/imprint/#contact"},
		{"release", "de", "https://tellmemoreaboutcore.com/de/core-starter/"},
		{"release", "ro", "https://tellmemoreaboutcore.com/core-starter/"},
		{"source", "de", "https://github.com/tmmac-core/corestarter"},
		{"https://evil.example/", "de", "https://tellmemoreaboutcore.com/de/"},
	} {
		if got := siteURL(c.page, c.lang); got != c.want {
			t.Errorf("siteURL(%q, %q) = %q, want %q", c.page, c.lang, got, c.want)
		}
	}
}

// TestPoolPageURL: the pool link only carries the saved, checked address and a
// region from the fixed list; without a valid saved address nothing opens.
func TestPoolPageURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if got := poolPageURL(path, "de"); got != "" {
		t.Errorf("no settings: %q, want none", got)
	}
	s := DefaultSettings()
	s.Address = "  corepass:CB292120D3ABC3B8330A26DAF4BEB94B1F6A9C7B4963 "
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	const base = "https://catchthatrabbit.com/coreid/cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963/"
	for _, c := range []struct{ region, want string }{
		{"de", base + "DE"},
		{"fi", base + "FI"},
		{"sg", base + "SG"},
		{"hk", base + "HK"},
		{"", base + "DE"},
		{"DE", base + "DE"}, // the list is lower case; anything else falls back
		{"FI", base + "DE"},
		{"../evil", base + "DE"},
		{"de/../../x?y=", base + "DE"},
	} {
		if got := poolPageURL(path, c.region); got != c.want {
			t.Errorf("poolPageURL(%q) = %q, want %q", c.region, got, c.want)
		}
	}
	// A broken address on record (hand-edited file) is dropped by Normalize.
	if err := os.WriteFile(path, []byte(`{"address":"cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4964"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := poolPageURL(path, "fi"); got != "" {
		t.Errorf("bad address: %q, want none", got)
	}
	if err := os.WriteFile(path, []byte(`{"address":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := poolPageURL(path, "fi"); got != "" {
		t.Errorf("broken file: %q, want none", got)
	}
}
