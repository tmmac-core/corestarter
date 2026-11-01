//go:build windows

package main

// Integration tests against the real embedded gocore on a real Windows machine.
// They start an offline node (no discovery, no peers, no UPnP) in a temp folder
// and touch HKCU, so they only run when CORE_NODE_STARTER_WINDOWS_IT=1 (CI).

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/tmmac-core/corestarter/internal/minercore"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestMain(m *testing.M) {
	if os.Getenv("CORE_NODE_STARTER_WINDOWS_IT") == "1" {
		extraNodeArgs = []string{"--nodiscover", "--maxpeers", "0", "--nat", "none"}
		// The real starter is a GUI program without a console; AttachConsole in
		// sendCtrlBreak only works from such a process.
		procFreeConsole.Call()
	}
	os.Exit(m.Run())
}

func integration(t *testing.T) {
	t.Helper()
	if os.Getenv("CORE_NODE_STARTER_WINDOWS_IT") != "1" {
		t.Skip("set CORE_NODE_STARTER_WINDOWS_IT=1 (starts gocore, writes HKCU)")
	}
}

func installedPaths(t *testing.T) paths {
	t.Helper()
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	if err := ensurePayload(p.bin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			raw, _ := os.ReadFile(p.nodeLog)
			t.Logf("gocore.log:\n%s", raw)
		}
	})
	return p
}

func waitReady(t *testing.T, np *nodeProc) {
	t.Helper()
	select {
	case <-np.ready:
	case <-np.exited:
		t.Fatalf("gocore exited with code %d before it was ready", np.exitCode())
	case <-time.After(5 * time.Minute):
		t.Fatal("gocore not ready after 5 minutes")
	}
}

// stopCleanly stops np and checks that gocore shut down on CTRL_BREAK
// instead of being terminated (TerminateProcess uses exit code 1).
func stopCleanly(t *testing.T, p paths, np *nodeProc) {
	t.Helper()
	start := time.Now()
	np.stop()
	took := time.Since(start)
	code := np.exitCode()
	t.Logf("stop took %v, exit code %d", took.Round(time.Millisecond), code)
	if code == 1 || took >= gracefulStopWait {
		t.Errorf("gocore was terminated instead of stopping on CTRL_BREAK")
	}
	raw, _ := os.ReadFile(p.nodeLog)
	if !strings.Contains(string(raw), "Got interrupt") {
		t.Errorf("gocore.log has no %q", "Got interrupt")
	}
}

func countNodes(t *testing.T, p paths) int {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	n := 0
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if h, ok := openOurNode(p, pe.ProcessID); ok {
			windows.CloseHandle(h)
			n++
		}
	}
	return n
}

func waitState(t *testing.T, sup *supervisor, want NodeState, notPID uint32) uint32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if st, pid, _, _ := sup.Current(); st == want && pid != notPID {
			return pid
		}
		time.Sleep(500 * time.Millisecond)
	}
	st, pid, _, _ := sup.Current()
	t.Fatalf("supervisor state %d pid %d, want state %d", st, pid, want)
	return 0
}

// The official release needs libstdc++-6.dll; with our bundled DLLs it must run.
func TestWindowsGocoreRunsWithBundledDLLs(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	out, err := exec.Command(p.gocore, "version").CombinedOutput()
	t.Logf("gocore version:\n%s", out)
	if err != nil {
		t.Fatalf("gocore version: %v", err)
	}
	if !strings.Contains(string(out), "Operating System: windows") {
		t.Error("unexpected gocore version output")
	}

	// A normal PC has no MinGW on PATH; the CI image does. So both runs get a
	// PATH with only the Windows folders, like a user's PC.
	sys, _ := windows.GetSystemDirectory()
	var cleanEnv []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			cleanEnv = append(cleanEnv, kv)
		}
	}
	cleanEnv = append(cleanEnv, "PATH="+sys+";"+filepath.Dir(sys))
	cmd := exec.Command(p.gocore, "version")
	cmd.Env = cleanEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("with clean PATH: %v %s", err, out)
	}

	// For the note to Core: the release exe alone, without our DLLs.
	alone := filepath.Join(t.TempDir(), "gocore.exe")
	if err := copyFile(p.gocore, alone); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(alone, "version")
	cmd.Env = cleanEnv
	out, err = cmd.CombinedOutput()
	t.Logf("release exe without bundled DLLs, clean PATH: err=%v output=%q", err, out)
	if err == nil {
		t.Log("NOTE: release exe ran without libstdc++-6.dll")
	}
}

func TestWindowsNodeStartQueryStop(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	np, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(np.handle)
	waitReady(t, np)

	snap, err := queryStatus(np.ipc, np.pid)
	if err != nil {
		t.Fatalf("status over named pipe: %v", err)
	}
	t.Logf("snapshot: %+v", *snap)
	if snap.Peers != 0 {
		t.Errorf("offline node reports %d peers", snap.Peers)
	}
	if _, err := queryStatus(np.ipc, np.pid+4); err == nil {
		t.Error("pipe accepted although the server pid does not match")
	}
	pid, h, err := findNode(p)
	if err != nil || pid != np.pid {
		t.Errorf("findNode = %d, %v; want %d", pid, err, np.pid)
	}
	if h != 0 {
		windows.CloseHandle(h)
	}

	stopCleanly(t, p, np)
}

// A stop request while gocore is still opening its database must wait for the
// signal handler instead of killing gocore.
func TestWindowsStopRightAfterStart(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	np, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(np.handle)
	stopCleanly(t, p, np)
}

// The starter was killed (Task Manager) while gocore kept running: a new
// supervisor adopts that gocore and never starts a second one.
func TestWindowsSupervisorAdoptsOrphan(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	orphan, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, orphan)

	sup := newSupervisor(p)
	sup.Start()
	pid := waitState(t, sup, NodeRunning, 0)
	if pid != orphan.pid {
		t.Errorf("supervisor runs pid %d, want adopted pid %d", pid, orphan.pid)
	}
	if n := countNodes(t, p); n != 1 {
		t.Errorf("%d gocore processes running, want 1", n)
	}
	_, _, ipc, _ := sup.Current()
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := queryStatus(ipc, pid); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("status of adopted node: %v", err)
		}
		time.Sleep(time.Second)
	}

	sup.Shutdown()
	select {
	case <-orphan.exited:
	case <-time.After(time.Minute):
		t.Fatal("adopted gocore still running after Shutdown")
	}
	if code := orphan.exitCode(); code == 1 {
		t.Error("adopted gocore was terminated instead of stopped")
	}
	windows.CloseHandle(orphan.handle)
	if exists(p.pidFile) {
		t.Error("pid file left behind")
	}
}

// A crashed gocore is started again after restartDelay.
func TestWindowsSupervisorRestartsCrashedNode(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	sup := newSupervisor(p)
	sup.Start()
	defer sup.Shutdown()
	first := waitState(t, sup, NodeRunning, 0)

	h, err := openProcess(first)
	if err != nil {
		t.Fatal(err)
	}
	windows.TerminateProcess(h, 7)
	windows.CloseHandle(h)

	second := waitState(t, sup, NodeRunning, first)
	t.Logf("crashed pid %d, restarted as pid %d", first, second)
	if n := countNodes(t, p); n != 1 {
		t.Errorf("%d gocore processes running, want 1", n)
	}
	sup.Shutdown()
	if st, _, _, _ := sup.Current(); st != NodeStopped {
		t.Errorf("state after Shutdown = %d", st)
	}
	if n := countNodes(t, p); n != 0 {
		t.Errorf("%d gocore processes still running after Shutdown", n)
	}
}

func TestWindowsAutostartUninstallEntryShortcut(t *testing.T) {
	integration(t)
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))

	if err := setAutostart(p, true); err != nil {
		t.Fatal(err)
	}
	if !autostartEnabled(p) {
		t.Error("autostart not enabled")
	}
	if err := setAutostart(p, false); err != nil {
		t.Fatal(err)
	}
	if autostartEnabled(p) {
		t.Error("autostart still enabled")
	}

	if err := registerUninstall(p); err != nil {
		t.Fatal(err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := k.GetStringValue("UninstallString")
	k.Close()
	if want := quoted(p.exe) + " --uninstall"; err != nil || v != want {
		t.Errorf("UninstallString = %q, %v; want %q", v, err, want)
	}
	unregisterUninstall()
	if k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKeyPath, registry.QUERY_VALUE); err == nil {
		k.Close()
		t.Error("uninstall entry still present")
	}

	// A shortcut under the old name (up to 0.8.x) must give way to the new one.
	lnk, _ := shortcutPath()
	old := filepath.Join(filepath.Dir(lnk), oldShortcutName)
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := createShortcut(p); err != nil {
		t.Fatalf("createShortcut %s: %v", lnk, err)
	}
	if !exists(lnk) {
		t.Errorf("shortcut %s missing", lnk)
	}
	if exists(old) {
		t.Errorf("old shortcut %s still present", old)
	}
	removeShortcut()
	if exists(lnk) {
		t.Error("shortcut still present")
	}
}

func TestWindowsUninstallKeepsKeystore(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	for _, f := range []string{
		filepath.Join(p.keystore, "UTC--key"),
		filepath.Join(p.data, "gocore", "chaindata", "000001.ldb"),
		filepath.Join(p.logs, "gocore.log"),
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	kept, err := removeInstall(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !kept || !exists(filepath.Join(p.keystore, "UTC--key")) {
		t.Error("keystore was not kept")
	}
	for _, gone := range []string{p.bin, p.logs, filepath.Join(p.data, "gocore")} {
		if exists(gone) {
			t.Errorf("%s not removed", gone)
		}
	}
}

// The real window and page: a script fills in the form like a user would, the
// page validates the address through Go and saves.
func TestWindowsSettingsWindow(t *testing.T) {
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	// WebView2's browser processes outlive the window for a moment and hold
	// files in ui\ (the uninstall retries the same way). Runs before t.TempDir's cleanup.
	t.Cleanup(func() {
		for i := 0; i < 40; i++ {
			if os.RemoveAll(p.root) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
	uiTestScript = `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(300);
		const mine = document.getElementById("mine");
		if (!mine.checked) mine.click();
		const a = document.getElementById("address");
		a.value = "CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963";
		a.dispatchEvent(new Event("input"));
		for (let i = 0; i < 50 && !document.getElementById("addrHint").classList.contains("good"); i++) await wait(100);
		` + jsAgree + `
		document.querySelector('[data-p="75"]').click();
		document.getElementById("save").click();
	})()`
	defer func() { uiTestScript = "" }()
	var got Settings
	saved := make(chan struct{}, 1)
	done := make(chan bool, 1)
	go func() {
		done <- runWindow(textsDE, p, uiSettings, "", func(s Settings) error {
			if err := s.Normalize(); err != nil {
				return err
			}
			got = s
			saved <- struct{}{}
			return nil
		})
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("window closed without saving")
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("window did not save within 2 minutes")
	}
	<-saved
	t.Logf("saved: %+v", got)
	if !got.Mine || got.Address != "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963" || got.Power != 75 || !got.Node || !got.nodeConsented() || !got.mineConsented() {
		t.Errorf("unexpected settings %+v", got)
	}
}

// The first start shows the window in install mode with its WebView2 data in
// %TEMP%; the settings test above does not cover that path.
func TestWindowsInstallWindow(t *testing.T) {
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	if c := installWindowPixel(t); c[0] > 0x40 || c[1] > 0x40 || c[2] > 0x40 {
		t.Errorf("window centre is %02x%02x%02x, want the dark page", c[0], c[1], c[2])
	}
}

// A real desktop can have a top-most window that draws nothing over the
// whole screen (GPU and recording overlays). Chromium's own occlusion check
// counts it as covering and stops painting: the white window from the PC
// test. The "unpatched" run only logs, to show the check bites at all.
func TestWindowsWindowUnderOverlay(t *testing.T) {
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	stop := startOverlay(t)
	defer stop()
	// The test before used the default switches; same race as below (public CI 01.11.).
	waitWebViewGone(t)
	t.Run("unpatched", func(t *testing.T) {
		args := uiBrowserArgs
		uiBrowserArgs = []string{"--disable-gpu"}
		defer func() { uiBrowserArgs = args }()
		c := installWindowPixel(t)
		t.Logf("without the occlusion switch the centre is %02x%02x%02x", c[0], c[1], c[2])
	})
	// The unpatched run used other browser switches. A new environment with
	// different switches on the same user data folder fails with 0x8007139f
	// while the old browser processes are still shutting down (seen in CI), and
	// go-webview2 then ends the whole test process. The app never switches
	// within one process; the test waits for the old ones to go.
	waitWebViewGone(t)
	t.Run("patched", func(t *testing.T) {
		if c := installWindowPixel(t); c[0] > 0x40 || c[1] > 0x40 || c[2] > 0x40 {
			t.Errorf("window centre is %02x%02x%02x under the overlay, want the dark page", c[0], c[1], c[2])
		}
	})
}

// installWindowPixel opens the first-start window, samples its centre while
// the page is up, lets the test script save and checks what the window left.
func installWindowPixel(t *testing.T) [3]byte {
	t.Helper()
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	uiNotesMu.Lock()
	uiNotes = nil
	uiNotesMu.Unlock()
	uiTestScript = `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(2500); // the pixel check below runs meanwhile, after the first frame
		if (!document.getElementById("intro").textContent) throw new Error("page not filled");
		` + jsAgree + `
		document.getElementById("save").click();
	})()`
	defer func() { uiTestScript = "" }()
	saved := make(chan Settings, 1)
	done := make(chan bool, 1)
	go func() {
		done <- runWindow(textsDE, p, uiInstall, "", func(s Settings) error {
			saved <- s
			return nil
		})
	}()
	// The page must really be drawn: a blank WebView2 still runs the script
	// above (seen on a real PC). The brand canvas is near black.
	pixel := make(chan [3]byte, 1)
	go func() {
		// Sample once the page reports its first frame (a slow runner can take
		// 4 s to load it), not after a fixed delay.
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			uiNotesMu.Lock()
			framed := strings.Contains(strings.Join(uiNotes, "\n"), "page: frame")
			uiNotesMu.Unlock()
			if framed {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
		c, err := windowPixel(uiClassName)
		if err != nil {
			t.Logf("pixel: %v", err)
			c = [3]byte{0xff, 0xff, 0xff}
		}
		pixel <- c
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("window closed without saving")
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("window did not save within 2 minutes")
	}
	c := <-pixel
	t.Logf("window centre %02x%02x%02x", c[0], c[1], c[2])
	uiNotesMu.Lock()
	notes := strings.Join(uiNotes, "\n")
	uiNotesMu.Unlock()
	t.Logf("notes:\n%s", notes)
	if !strings.Contains(notes, "page: frame") {
		t.Error("notes miss the page's first frame")
	}
	if exists(uiDiagPath()) {
		t.Error("diag file written although the window worked")
	}
	if s := <-saved; !s.Node || s.Mine {
		t.Errorf("unexpected default settings %+v", s)
	}
	if exists(p.root) {
		t.Error("install window wrote into the program folder before the install")
	}
	return c
}

// startOverlay puts a top-most popup over the whole screen that draws nothing
// (no redirection bitmap), like the overlays of GPU tools and recorders.
func startOverlay(t *testing.T) (stop func()) {
	ready := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		user := windows.NewLazySystemDLL("user32.dll")
		cls, _ := windows.UTF16PtrFromString("CoreStarterTestOverlay")
		wc := wndClassEx{wndProc: procDefWindowProcW.Addr(), className: cls}
		wc.size = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		metrics := user.NewProc("GetSystemMetrics")
		cx, _, _ := metrics.Call(0) // SM_CXSCREEN
		cy, _, _ := metrics.Call(1) // SM_CYSCREEN
		const exTopmost, exNoRedirectionBitmap = 0x00000008, 0x00200000
		const wsPopup, wsVisible = 0x80000000, 0x10000000
		h, _, _ := procCreateWindowExW.Call(exTopmost|exNoRedirectionBitmap, uintptr(unsafe.Pointer(cls)), 0,
			wsPopup|wsVisible, 0, 0, cx, cy, 0, 0, 0, 0)
		ready <- h
		if h == 0 {
			return
		}
		var m winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		procDestroyWindow.Call(h)
	}()
	h := <-ready
	if h == 0 {
		t.Fatal("overlay window not created")
	}
	const wmQuit = 0x0012
	return func() { procPostMessageW.Call(h, wmQuit, 0, 0) }
}

// The miner view (tray menu "Show miner") shows the tray's status and follows
// miner.log. The script closes the window only once both arrived.
// minerWindow opens the window on the miner view with saved settings s, a
// status.json of a running miner and a two-line miner.log, runs script in the
// page and waits until the page closes the window. It returns the app paths.
func minerWindow(t *testing.T, s Settings, m *minercore.MinerStatus, script, failure string) paths {
	t.Helper()
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	t.Cleanup(func() {
		for i := 0; i < 40; i++ {
			if os.RemoveAll(p.root) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
	if err := os.MkdirAll(p.logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(p.settings, s); err != nil {
		t.Fatal(err)
	}
	as := AppStatus{Updated: time.Now(), MinerOn: true, MinerLine: "Minen: 3.228 H/s", LastShare: time.Now().Unix() - 30,
		History: []float64{3100, 3200, 3228}, Miner: m}
	b, _ := json.Marshal(as)
	if err := os.WriteFile(p.status, b, 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.minerLog, []byte("2026/10/18 10:00:00 connected to de as pc-test\n2026/10/18 10:01:00 share accepted (5)\n"), 0o644)
	uiTestScript = script
	defer func() { uiTestScript = "" }()
	done := make(chan bool, 1)
	go func() {
		done <- runWindow(textsFor(s.Lang), p, uiSettings, "miner", func(Settings) error { return errors.New("no save expected") })
	}()
	select {
	case <-done:
	case <-time.After(time.Minute):
		// Close it, else its window mutex makes every later window test return at once.
		cls, _ := windows.UTF16PtrFromString(uiClassName)
		if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); h != 0 {
			procPostMessageW.Call(h, wmClose, 0, 0)
		}
		t.Fatal(failure + " (window still open)")
	}
	return p
}

var testMinerSettings = Settings{Node: true, Mine: true, Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "pc-test", Power: 50, Lang: "de", Consent: agreedConsent()}

func TestWindowsMinerView(t *testing.T) {
	m := &minercore.MinerStatus{State: "mining", Hashrate: 3228, Threads: 3, MaxThreads: 12, Accepted: 5, Pool: "de",
		ThreadRates: []float64{1080, 1090, 600, 0, 0, 0, 0, 0, 0, 0, 0, 0}, ShareDiff: 339960, AnswerMs: 11}
	minerWindow(t, testMinerSettings, m, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		for (let i = 0; i < 100; i++) {
			const rate = document.getElementById("mvRate").textContent;
			const log = document.getElementById("mvLog").textContent;
			const shown = !document.getElementById("minerView").classList.contains("hidden");
			const tiles = document.querySelectorAll("#mvThreadGrid .th"), slow = document.querySelectorAll("#mvThreadGrid .th.slow"), idle = document.querySelectorAll("#mvThreadGrid .th.idle");
			const plain = document.getElementById("mvPlain").textContent;
			if (shown && rate === "3.228" && log.includes("Share angenommen (5 insgesamt)") && document.getElementById("mvLine").getAttribute("points") && tiles.length === 12 && slow.length === 1 && idle.length === 9 && document.getElementById("mvDiff").textContent.includes("340.000") && plain.startsWith("Alles in Ordnung") && !document.getElementById("mvMore").open) {
				document.getElementById("mvClose").click();
				return;
			}
			await wait(100);
		}
	})()`, "miner view did not show the status, the plain line and the log")
}

// The language from settings.json reaches the page (texts and the translated
// miner log), and switching it in the window writes it back to settings.json.
func TestWindowsLanguage(t *testing.T) {
	s := testMinerSettings
	s.Lang = "ro"
	m := &minercore.MinerStatus{State: "mining", Hashrate: 3228, Threads: 3, MaxThreads: 12, Accepted: 5, Pool: "de"}
	p := minerWindow(t, s, m, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		const log = () => document.getElementById("mvLog").textContent;
		let i = 0;
		for (; i < 100 && !(document.documentElement.lang === "ro" && log().includes("Share acceptat (5 în total)") && log().includes("Conectat la de ca pc-test")); i++) await wait(100);
		if (i === 100) return;
		const sel = document.getElementById("langSel");
		sel.value = "nl";
		sel.dispatchEvent(new Event("change"));
		for (i = 0; i < 100 && !log().includes("Share geaccepteerd (5 in totaal)"); i++) await wait(100);
		if (i === 100) return;
		await wait(500); // setlang has written settings.json
		document.getElementById("mvClose").click();
	})()`, "window did not show the Romanian log or did not switch to Dutch")
	got, err := LoadSettings(p.settings)
	if err != nil || got.Lang != "nl" {
		t.Errorf("settings after switching: lang %q, %v; want nl", got.Lang, err)
	}
}

// "Minen pausieren" in the miner view switches mining off in settings.json
// and keeps the address for "fortsetzen".
func TestWindowsMinerPause(t *testing.T) {
	m := &minercore.MinerStatus{State: "mining", Hashrate: 3228, Threads: 3, MaxThreads: 12, Accepted: 5, Pool: "de"}
	p := minerWindow(t, testMinerSettings, m, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		const b = document.getElementById("mvPause");
		let i = 0;
		for (; i < 100 && (b.classList.contains("hidden") || b.textContent !== "Minen pausieren"); i++) await wait(100);
		if (i === 100) return;
		b.click();
		await wait(800); // setmine has written settings.json
		document.getElementById("mvClose").click();
	})()`, "pause button did not show")
	got, err := LoadSettings(p.settings)
	if err != nil || got.Mine || got.Address != testMinerSettings.Address {
		t.Errorf("settings after pausing: %+v, %v; want mine off, address kept", got, err)
	}
}

// A page that never reaches the bridge must not leave a blank window open.
// A launcher can pass a show mode that the first ShowWindow call obeys
// instead of ours (Norton started the exe after its scan: minimized). Go's
// HideWindow passes SW_HIDE the same way; the window must still come up
// visible and not minimized. The child process is this test binary.
func TestWindowsWindowStartsVisible(t *testing.T) {
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	if os.Getenv("CS_SHOW_CHILD") == "1" {
		if os.Getenv("CS_SHOW_ONCE") == "1" {
			uiShowCalls, uiShowCmd = 1, 5 // v0.5.3 and before: one ShowWindow(SW_SHOW)
		}
		showChild(t)
		return
	}
	run := func(once bool) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsWindowStartsVisible$", "-test.v")
		cmd.Env = append(os.Environ(), "CS_SHOW_CHILD=1")
		if once {
			cmd.Env = append(cmd.Env, "CS_SHOW_ONCE=1")
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // STARTF_USESHOWWINDOW + SW_HIDE
		out, err := cmd.CombinedOutput()
		t.Logf("child (once=%v):\n%s", once, out)
		return err
	}
	if err := run(true); err == nil {
		t.Log("with the old single ShowWindow(SW_SHOW) the window came up anyway (launcher mode not applied here)")
	} else {
		t.Log("with the old single ShowWindow(SW_SHOW) the window stayed hidden or minimized: the check bites")
	}
	if err := run(false); err != nil {
		t.Errorf("window not visible after a start with SW_HIDE: %v", err)
	}
}

func showChild(t *testing.T) {
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	uiTestScript = `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(4000);
		` + jsAgree + `
		document.getElementById("save").click();
	})()`
	defer func() { uiTestScript = "" }()
	state := make(chan string, 1)
	go func() {
		time.Sleep(3 * time.Second)
		user := windows.NewLazySystemDLL("user32.dll")
		cls, _ := windows.UTF16PtrFromString(uiClassName)
		h, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(cls)), 0)
		vis, _, _ := user.NewProc("IsWindowVisible").Call(h)
		icon, _, _ := user.NewProc("IsIconic").Call(h)
		switch {
		case h == 0:
			state <- "no window"
		case vis == 0:
			state <- "hidden"
		case icon != 0:
			state <- "minimized"
		default:
			state <- ""
		}
	}()
	runWindow(textsDE, p, uiInstall, "", func(Settings) error { return nil })
	if s := <-state; s != "" {
		t.Fatalf("window %s", s)
	}
}

func TestWindowsWindowWatchdog(t *testing.T) {
	integration(t)
	if !webView2Available() {
		t.Skip("no WebView2 runtime")
	}
	p := pathsAt(filepath.Join(t.TempDir(), appFolder))
	t.Cleanup(func() {
		for i := 0; i < 40; i++ {
			if os.RemoveAll(p.root) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
	page, wait := uiPage, uiWatchdog
	// The case from the PC test: the script runs and talks to the bridge, but
	// no frame is ever reported as drawn.
	uiPage, uiWatchdog = `<!doctype html><title>x</title><script>chrome.webview.postMessage(JSON.stringify({id: 1, cmd: "init", arg: null}))</script>`, 3*time.Second
	defer func() { uiPage, uiWatchdog = page, wait }()
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		w := &window{t: textsDE, p: p, mode: uiSettings, onSave: func(Settings) error { return nil }}
		theWindow = w
		errc <- w.run()
		theWindow = nil
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, errNoPage) {
			t.Fatalf("run = %v, want errNoPage", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("watchdog did not close the window")
	}
}

// windowPixel renders the window of the given class (PrintWindow with full
// content, which includes WebView2's composition) and returns the RGB of the
// pixel in the middle of its client area.
func windowPixel(class string) ([3]byte, error) {
	user := windows.NewLazySystemDLL("user32.dll")
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	cls, _ := windows.UTF16PtrFromString(class)
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(cls)), 0)
	if hwnd == 0 {
		return [3]byte{}, errors.New("window not found")
	}
	var rc struct{ left, top, right, bottom int32 }
	user.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := uintptr(rc.right-rc.left), uintptr(rc.bottom-rc.top)
	wdc, _, _ := user.NewProc("GetDC").Call(hwnd)
	defer user.NewProc("ReleaseDC").Call(hwnd, wdc)
	mdc, _, _ := gdi.NewProc("CreateCompatibleDC").Call(wdc)
	defer gdi.NewProc("DeleteDC").Call(mdc)
	bmp, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(wdc, w, h)
	defer gdi.NewProc("DeleteObject").Call(bmp)
	gdi.NewProc("SelectObject").Call(mdc, bmp)
	// Magenta first: a capture that draws nothing must not pass as the dark page
	// (a fresh bitmap is black).
	magenta, _, _ := gdi.NewProc("CreateSolidBrush").Call(0x00ff00ff)
	defer gdi.NewProc("DeleteObject").Call(magenta)
	full := struct{ left, top, right, bottom int32 }{0, 0, int32(w), int32(h)}
	user.NewProc("FillRect").Call(mdc, uintptr(unsafe.Pointer(&full)), magenta)
	const pwClientOnly, pwRenderFullContent = 1, 2
	if r, _, err := user.NewProc("PrintWindow").Call(hwnd, mdc, pwClientOnly|pwRenderFullContent); r == 0 {
		return [3]byte{}, err
	}
	c, _, _ := gdi.NewProc("GetPixel").Call(mdc, w/2, h/2)
	// PrintWindow asks the window to draw itself; the screen shows what DWM
	// really composed. Both go to the log, the screen one is the user's view.
	pt := struct{ x, y int32 }{int32(w / 2), int32(h / 2)}
	user.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	sdc, _, _ := user.NewProc("GetDC").Call(0)
	sc, _, _ := gdi.NewProc("GetPixel").Call(sdc, uintptr(pt.x), uintptr(pt.y))
	user.NewProc("ReleaseDC").Call(0, sdc)
	log.Printf("pixel: window %06x, screen %06x (0x00BBGGRR)", c&0xffffff, sc&0xffffff)
	return [3]byte{byte(c), byte(c >> 8), byte(c >> 16)}, nil // COLORREF is 0x00BBGGRR
}

// countProcesses counts running processes with the given image name.
func countProcesses(image string) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return -1
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	n := 0
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), image) {
			// An exited process stays listed while another program (e.g. the
			// virus scanner) still holds a handle to it; only running ones count.
			if code, ok := exitCodeOf(pe.ProcessID); ok && code != stillActive {
				log.Printf("%s pid %d already exited (code %d), still listed", image, pe.ProcessID, code)
				continue
			}
			n++
		}
	}
	return n
}

func exitCodeOf(pid uint32) (uint32, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return 0, false
	}
	return code, true
}

func TestWindowsPortInUseFromLog(t *testing.T) {
	log := filepath.Join(t.TempDir(), "gocore.log")
	os.WriteFile(log, []byte("INFO start\nFatal: Error starting protocol stack: listen tcp :30300: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.\n"), 0o644)
	if !portInUse(log) {
		t.Fatal("bind failure not recognised")
	}
	os.WriteFile(log, []byte("INFO start\nFatal: something else\n"), 0o644)
	if portInUse(log) {
		t.Fatal("other failure taken for a busy port")
	}
	// A bind error of an earlier run does not count for the current one.
	os.WriteFile(log, []byte(startMarker+" port 30300\nbind: Only one usage of each socket address\n"+startMarker+" port 30301\nFatal: database corrupted\n"), 0o644)
	if portInUse(log) {
		t.Fatal("bind error of an earlier run counted")
	}
	if args := strings.Join(nodeArgs(paths{}, "x", 30301), " "); !strings.Contains(args, "--port 30301") {
		t.Fatalf("args %s", args)
	}
	if args := strings.Join(nodeArgs(paths{}, "x", defaultP2PPort), " "); strings.Contains(args, "--port") {
		t.Fatalf("default port passed: %s", args)
	}
}

// waitWebViewGone waits up to 20 s until no WebView2 browser process of this
// test is left.
func waitWebViewGone(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if countProcesses("msedgewebview2.exe") <= 0 {
			return
		}
	}
	t.Logf("WebView2 processes still running: %d", countProcesses("msedgewebview2.exe"))
}
