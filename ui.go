package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

//go:embed ui/index.html
var uiPage string

//go:embed assets/app.png
var uiIcon []byte

var errNoPage = errors.New("page did not draw") // the window watchdog gave up on the page

type uiMode string

const (
	uiInstall  uiMode = "install"  // first start from the download: nothing is installed before "Start"
	uiSettings uiMode = "settings" // installed; changes go to the running tray
)

// window is the one settings window of this process; nativeWin holds what the
// system's web view needs (WebView2 on Windows, WebKitGTK on Linux).
type window struct {
	nativeWin
	t      texts
	p      paths
	mode   uiMode
	view   string // "miner": start on the miner view; under mu
	onSave func(Settings) error
	origin string // source of the first message: our page; anything else is ignored
	failed error  // set by the watchdog on the UI thread; run returns it
	framed bool   // the page reported its first drawn frame

	mu         sync.Mutex
	replies    []string // scripts to run on the UI thread (wmApp)
	calls      []func() // Go functions to run on the UI thread (wmApp)
	looping    bool     // message loop running, so wmApp gets handled
	saved      bool
	saving     bool // an install/save runs; closing now would cut it off mid-copy
	noQuit     bool // window destroyed during setup; no WM_QUIT
	closeAsked bool // a close came while saving; done once the save ends
	embedded   bool // the view exists; closing before would break Embed
}

// uiNotes keeps the WebView2 events of this run. Before the install the log
// goes nowhere; install() writes them to starter.log once it exists.
var (
	uiNotesMu sync.Mutex
	uiNotes   []string
)

func uiNote(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	log.Print(s)
	uiNotesMu.Lock()
	if len(uiNotes) < 50 {
		uiNotes = append(uiNotes, s)
	}
	uiNotesMu.Unlock()
}

// flushUINotes writes the notes from before the log existed.
func flushUINotes() {
	uiNotesMu.Lock()
	notes := append([]string(nil), uiNotes...)
	uiNotesMu.Unlock()
	for _, s := range notes {
		log.Printf("before install: %s", s)
	}
}

// uiTestScript is only set by the integration tests: it runs in the
// page after "init" and the window closes after a successful save.
var uiTestScript string

func (w *window) currentView() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.view
}

// pageMode: after a successful install the page is the settings page; a page
// loaded again (macOS reloads it when WebKit's content process ended) must not
// show the install form once more.
func (w *window) pageMode() uiMode {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.mode == uiInstall && w.saved {
		return uiSettings
	}
	return w.mode
}

// reply queues a script for the UI thread; safe from any goroutine.
func (w *window) reply(id int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte("null")
	}
	w.mu.Lock()
	w.replies = append(w.replies, fmt.Sprintf("window.__reply(%d,%s)", id, b))
	w.mu.Unlock()
	w.wake()
}

// asciiJS writes every non-ASCII character of a script as \uXXXX (UTF-16, as
// JavaScript reads it), which means the same in strings, templates, regular
// expressions and names. WebKitGTK 2.52 (openSUSE Leap 15.6) crashes its web
// process on a run_javascript script with raw UTF-8, e.g. the NOTICE text or a
// home folder with an umlaut in the init reply.
func asciiJS(js string) string {
	i := 0
	for i < len(js) && js[i] < utf8.RuneSelf {
		i++
	}
	if i == len(js) {
		return js
	}
	var b strings.Builder
	b.Grow(len(js) + 64)
	b.WriteString(js[:i])
	for _, r := range js[i:] {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// scriptPiece: WebKitGTK 2.52.1 (openSUSE Leap 15.6) crashes its web process
// a few seconds after a run_javascript script over 16 KB, whatever it holds;
// the init reply carries the window icon (about 23 KB). splitScript sends a
// longer script as string pieces of this size and runs it as an inline script
// element (the page's CSP allows inline scripts, not eval); the pieces arrive
// in order.
const scriptPiece = 4096

func splitScript(js string) []string {
	if len(js) <= 2*scriptPiece {
		return []string{js}
	}
	out := []string{"window.__cs = []"}
	for i := 0; i < len(js); i += scriptPiece {
		var b strings.Builder
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)                      // < > & stay one byte: a piece stays under 2x its size
		e.Encode(js[i:min(i+scriptPiece, len(js))]) // js is ASCII (asciiJS): no rune is cut
		out = append(out, "window.__cs.push("+strings.TrimSuffix(b.String(), "\n")+")")
	}
	return append(out, `{ const s = document.createElement("script"); s.textContent = window.__cs.join(""); window.__cs = [];`+
		` document.head.appendChild(s); s.remove(); }`)
}

func (w *window) flushReplies() {
	w.mu.Lock()
	scripts := w.replies
	w.replies = nil
	calls := w.calls
	w.calls = nil
	w.mu.Unlock()
	for _, f := range calls {
		f()
	}
	for _, s := range scripts {
		w.eval(s)
	}
}

type uiRequest struct {
	ID  int             `json:"id"`
	Cmd string          `json:"cmd"`
	Arg json.RawMessage `json:"arg"`
}

// handle runs on the UI thread; slow work goes to a goroutine and replies later.
func (w *window) handle(msg string) {
	var req uiRequest
	if err := json.Unmarshal([]byte(msg), &req); err != nil {
		return
	}
	switch req.Cmd {
	case "frame":
		uiNote("page: frame %s", req.Arg)
		var a struct{ Later bool }
		json.Unmarshal(req.Arg, &a)
		// Only the requestAnimationFrame report proves a drawn frame; the later
		// one comes from a timer, which also runs in a view that never paints.
		if !a.Later && !w.framed && w.failed == nil {
			// First frame drawn: re-apply the bounds once more, the cheap cure
			// for a view that only shows after the window was moved or resized.
			w.framed = true
			w.firstFrame()
		}
		w.reply(req.ID, nil)
	case "init":
		uiNote("page: init %s", req.Arg)
		go func() { // initData walks the chain data, which can take a while
			w.reply(req.ID, w.initData())
			if uiTestScript != "" {
				w.mu.Lock()
				w.replies = append(w.replies, uiTestScript)
				w.mu.Unlock()
				w.wake()
			}
		}()
	case "minerlog":
		var a struct{ From int64 }
		json.Unmarshal(req.Arg, &a)
		go func() { // file I/O off the UI thread
			c, err := readLogChunk(w.p.minerLog, a.From)
			if err != nil {
				c = logChunk{Next: -1} // no log yet: ask again from the tail
			}
			w.reply(req.ID, c)
		}()
	case "validate":
		var a struct{ Address string }
		json.Unmarshal(req.Arg, &a)
		w.reply(req.ID, validateAddress(a.Address))
	case "status":
		var st *AppStatus
		if raw, err := os.ReadFile(w.p.status); err == nil {
			st = &AppStatus{}
			// The tray rewrites it every 5 s; an old file is from a tray that is gone.
			if json.Unmarshal(raw, st) != nil || time.Since(st.Updated) > 30*time.Second {
				st = nil
			}
		}
		w.reply(req.ID, st)
	case "setlang":
		// The language switches at once: dialogs of this window follow, and an
		// installed app keeps it right away (tray menu included), without the
		// other, maybe half-edited fields of the form.
		var a struct{ Lang string }
		json.Unmarshal(req.Arg, &a)
		if !slices.Contains(uiLangs, a.Lang) {
			a.Lang = ""
		}
		w.t = textsFor(a.Lang)
		// Not installed yet (no settings): nothing to keep. settingsMu is held
		// only for the file work, so the UI thread waits at most for one write.
		changed, err := updateSettings(w.p.settings, func(s *Settings) (bool, error) {
			if s.Lang == a.Lang {
				return false, nil
			}
			s.Lang = a.Lang
			return true, nil
		})
		switch {
		case changed && err != nil:
			log.Printf("language: %v", err)
		case changed:
			reloadTray()
		}
		w.reply(req.ID, map[string]string{"lang": w.t.lang})
	case "save":
		var a struct {
			Settings
			Agree struct{ Node, Mine bool } `json:"agree"` // the notes' tick box
		}
		if err := json.Unmarshal(req.Arg, &a); err != nil {
			w.reply(req.ID, map[string]string{"error": err.Error()})
			return
		}
		s, prev := a.Settings, savedSettings(w.p.settings)
		s.Consent = Consent{} // never what the page sends
		if prev != nil {
			s.Consent = prev.Consent
		}
		s.Consent.agree(a.Agree.Node, a.Agree.Mine && s.Mine, time.Now())
		err := s.consentProblem(prev)
		if err == nil {
			err = s.autostartProblem(prev, a.Agree.Node)
		}
		if err != nil {
			w.reply(req.ID, map[string]string{"error": err.Error(), "code": errorCode(err)})
			return
		}
		w.mu.Lock()
		if w.saving {
			w.mu.Unlock()
			w.reply(req.ID, map[string]string{"error": "busy", "code": "busy"})
			return
		}
		w.saving = true
		w.mu.Unlock()
		go func() {
			err := w.onSave(s)
			w.mu.Lock()
			w.saving = false
			closeNow := w.closeAsked
			w.mu.Unlock()
			if closeNow {
				defer w.closeWindow()
			}
			if err != nil {
				log.Printf("save: %v", err)
				w.reply(req.ID, map[string]string{"error": err.Error(), "code": errorCode(err)})
				return
			}
			w.mu.Lock()
			w.saved = true
			w.mu.Unlock()
			w.reply(req.ID, map[string]bool{"ok": true})
			if uiTestScript != "" {
				w.closeWindow()
			}
		}()
	case "open":
		var a struct{ What, Lang string }
		json.Unmarshal(req.Arg, &a)
		switch a.What {
		case "data":
			os.MkdirAll(w.p.data, 0o700) // missing when only mining; holds the keystore
			shellOpen(w.p.data)
		case "logs":
			shellOpen(w.p.logs)
		case "licenses":
			shellOpen(licenseDir(w.p))
		case "privacy":
			// Opens the browser on the user's click; the starter itself connects nowhere.
			shellOpen(privacyURL(a.Lang))
		case "site", "imprint", "feedback", "release", "source":
			shellOpen(siteURL(a.What, a.Lang))
		}
		w.reply(req.ID, true)
	case "setmine":
		// Pause or resume from the miner view: only the mine switch changes,
		// the address and the rest of the saved settings stay.
		// The tray must act on it like on "save": a change only written to
		// settings.json would leave the miner as it was, without a word.
		var a struct{ Mine bool }
		json.Unmarshal(req.Arg, &a)
		go func() { // startTray can wait for a quitting tray: off the UI thread
			_, err := updateSettings(w.p.settings, func(s *Settings) (bool, error) {
				s.Mine = a.Mine
				if a.Mine && !s.mineConsented() {
					return false, errConsent // pausing always works, resuming needs the current notes
				}
				return true, s.Normalize()
			})
			if err == nil {
				err = applyToTray(w.p)
			}
			if err != nil {
				log.Printf("setmine: %v", err)
				w.reply(req.ID, map[string]string{"error": err.Error(), "code": errorCode(err)})
				return
			}
			w.reply(req.ID, map[string]bool{"ok": true})
		}()
	case "quit":
		// The same request an update or "Apps & Features" sends; the tray
		// stops node and miner and exits. Without a tray there is nothing to stop.
		if !trayAccepting() || !askTrayQuit() {
			w.reply(req.ID, map[string]string{"error": "no tray", "code": "notray"})
			return
		}
		w.reply(req.ID, map[string]bool{"ok": true})
		w.closeWindow()
	case "uninstall":
		// The system's own uninstall (Windows: the installed exe asks, closes
		// this window and stops the tray).
		if err := startUninstall(w.p); err != nil {
			log.Printf("uninstall: %v", err)
			w.reply(req.ID, map[string]string{"error": err.Error()})
			return
		}
		w.reply(req.ID, map[string]bool{"ok": true})
	case "close":
		w.closeWindow()
	}
}

type uiInit struct {
	Mode     uiMode      `json:"mode"`
	View     string      `json:"view,omitempty"`
	Lang     string      `json:"lang"`    // effective language of the window
	SysLang  string      `json:"sysLang"` // Windows' language, for "automatic"
	Version  string      `json:"version"`
	Gocore   string      `json:"gocore"`
	Miner    bool        `json:"miner"`
	Settings Settings    `json:"settings"`
	HasSaved bool        `json:"hasSaved"`
	FreeGB   float64     `json:"freeGB"`
	NeedGB   int         `json:"needGB"`
	DataGB   float64     `json:"dataGB"`
	Support  string      `json:"supportAddress"`
	Icon     string      `json:"icon"` // app symbol (assets/app.png, logos repo derived/core-starter/off.svg) as data URI
	RAMGB    float64     `json:"ramGB"`
	ARM64    bool        `json:"arm64"`
	Threads  map[int]int `json:"threads"`  // power (25/50/75/100) -> mining threads, as the miner counts them
	Platform string      `json:"platform"` // "windows" or "linux": the page picks its system texts
	// The notes still to accept (consent.go): the page shows its consent card
	// for these, and Go refuses to save without them.
	NeedNode bool   `json:"needNode"`
	NeedMine bool   `json:"needMine"`
	Notice   string `json:"notice"` // NOTICE.md (licence and liability), readable before installing
}

func (w *window) initData() uiInit {
	d := uiInit{
		Mode: w.pageMode(), View: w.currentView(), Lang: w.t.lang, SysLang: systemTexts().lang, Version: appVersion, Gocore: gocoreVersion, Miner: minerAvailable,
		NeedGB: needGB, Support: minercore.SupportAddress,
		Icon:     "data:image/png;base64," + base64.StdEncoding.EncodeToString(uiIcon),
		Platform: runtime.GOOS,
	}
	d.RAMGB = float64(totalMemory()) / (1 << 30)
	// core-miner uses every logical processor (RandomY's scratchpad is small).
	d.Threads = map[int]int{}
	for _, pct := range []int{25, 50, 75, 100} {
		d.Threads[pct] = minercore.ThreadsFor(pct, runtime.NumCPU())
	}
	d.ARM64 = onARM64()
	if s, err := LoadSettings(w.p.settings); err == nil {
		d.Settings, d.HasSaved = s, true
		// Show what Windows really does: the user may have switched the entry
		// off in Task Manager since.
		d.Settings.Autostart = autostartEnabled(w.p)
	} else {
		d.Settings = DefaultSettings()
	}
	d.NeedNode, d.NeedMine = !d.Settings.nodeConsented(), !d.Settings.mineConsented()
	d.Notice = noticeText
	base := filepath.Dir(w.p.root)
	if free, err := freeBytes(base); err == nil {
		d.FreeGB = float64(free) / (1 << 30)
	}
	if exists(w.p.data) {
		d.DataGB = float64(dirSize(w.p.data)) / (1 << 30)
	}
	return d
}

type addrResult struct {
	OK      bool   `json:"ok"`
	Address string `json:"address,omitempty"`
	Code    string `json:"code,omitempty"` // "format", "checksum", "network"
}

func validateAddress(in string) addrResult {
	a, err := minercore.ParseICAN(in)
	switch {
	case errors.Is(err, minercore.ErrAddressChecksum):
		return addrResult{Code: "checksum"}
	case err != nil:
		return addrResult{Code: "format"}
	case a.Network != minercore.Mainnet:
		return addrResult{Code: "network"}
	}
	return addrResult{OK: true, Address: a.Address}
}

// Errors the page shows with its own text.
var (
	errDollarPath = errors.New("user folder path contains '$'")
	errNoSpace    = errors.New("not enough free disk space")
)

func errorCode(err error) string {
	switch {
	case errors.Is(err, errDollarPath):
		return "path"
	case errors.Is(err, errNoSpace):
		return "space"
	case errors.Is(err, errConsent):
		return "consent"
	case errors.Is(err, minercore.ErrAddressChecksum), errors.Is(err, minercore.ErrAddressFormat), errors.Is(err, minercore.ErrNotMainnet):
		return "address"
	}
	return "other"
}

// saveFromWindow is onSave for the installed program: write the settings, set
// autostart, and tell the tray (or start it if it is not running).
func saveFromWindow(p paths, s Settings) error {
	// Switching the node on later needs the same space as a first install.
	if err := preflight(p, s.Node); err != nil {
		return err
	}
	if err := SaveSettings(p.settings, s); err != nil {
		return err
	}
	// Write only a real change, so a Task Manager choice survives other saves.
	if s.Autostart != autostartEnabled(p) {
		if err := setAutostart(p, s.Autostart); err != nil {
			log.Printf("autostart: %v", err)
		}
	}
	return applyToTray(p)
}

// applyToTray makes the tray act on the saved settings: a running one reads
// them again; without one (or with one that is quitting) startTray starts it
// and waits for the old one first (Windows: --handover).
func applyToTray(p paths) error {
	if trayAccepting() {
		askTrayReload()
		return nil
	}
	return startTray(p)
}

// reloadTray tells a running tray to read the settings again.
func reloadTray() {
	if trayAccepting() {
		askTrayReload()
	}
}

// preflight checks what would make the install fail later.
func preflight(p paths, node bool) error {
	// gocore expands environment variables in --datadir (os.ExpandEnv).
	if strings.Contains(p.data, "$") {
		return errDollarPath
	}
	if node && !exists(p.data) {
		if free, err := freeBytes(filepath.Dir(p.root)); err == nil && free < needGB<<30 {
			return fmt.Errorf("%w: %d GB free, %d GB needed", errNoSpace, free>>30, needGB)
		}
	}
	return nil
}
