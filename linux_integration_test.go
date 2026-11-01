//go:build linux

package main

// Integration tests against the installed package (/usr/lib/core-starter) in
// distro containers. They start an offline node (no discovery, no peers, no
// UPnP) in a temp folder, so they only run when CORE_STARTER_LINUX_IT=1 (CI).
// The window tests need a display (xvfb-run).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const installedBin = "/usr/lib/core-starter"

// helperMiner is set by linux_miner_test.go (not in -tags nominer builds).
var helperMiner func()

func TestMain(m *testing.M) {
	if os.Getenv("CORE_STARTER_LINUX_IT") == "1" {
		extraNodeArgs = []string{"--nodiscover", "--maxpeers", "0", "--nat", "none"}
	}
	// The real handler re-executes the process: here that is the test binary,
	// which would start every test again.
	onWindowFailed = func(texts, paths) { uiNote("window failed (test: no restart)") }
	if os.Getenv("CORE_STARTER_TEST_HELPER") == "miner" && helperMiner != nil {
		helperMiner() // never returns
	}
	os.Exit(m.Run())
}

func integration(t *testing.T) {
	t.Helper()
	if os.Getenv("CORE_STARTER_LINUX_IT") != "1" {
		t.Skip("set CORE_STARTER_LINUX_IT=1 (starts gocore from the installed package)")
	}
}

func testPaths(t *testing.T) paths {
	t.Helper()
	tmp := t.TempDir()
	run := filepath.Join(tmp, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	p := pathsAt(filepath.Join(tmp, "data"), filepath.Join(tmp, "config"), filepath.Join(tmp, "state"), run, installedBin)
	appP = p
	t.Cleanup(func() {
		if t.Failed() {
			raw, _ := os.ReadFile(p.nodeLog)
			t.Logf("gocore.log:\n%s", raw)
		}
	})
	return p
}

func installedPaths(t *testing.T) paths {
	t.Helper()
	integration(t)
	p := testPaths(t)
	if !exists(p.gocore) {
		t.Fatalf("%s missing: is the package installed?", p.gocore)
	}
	return p
}

func waitReady(t *testing.T, np *nodeProc) {
	t.Helper()
	select {
	case <-np.ready:
	case <-np.exited:
		t.Fatalf("gocore exited with code %d before it was ready", np.exitCode())
	case <-time.After(3 * time.Minute):
		t.Fatal("gocore not ready after 3 minutes")
	}
}

func stopCleanly(t *testing.T, np *nodeProc) {
	t.Helper()
	t0 := time.Now()
	np.stop()
	took := time.Since(t0)
	if code := np.exitCode(); code != 0 {
		t.Fatalf("gocore exit code %d after %v, want a clean stop", code, took)
	}
	t.Logf("gocore stopped cleanly in %v", took)
}

func waitState(t *testing.T, sup *supervisor, want NodeState, notPID uint32) uint32 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		st, pid, _, _ := sup.Current()
		if st == want && pid != notPID {
			return pid
		}
		time.Sleep(500 * time.Millisecond)
	}
	st, pid, _, _ := sup.Current()
	t.Fatalf("state %v pid %d, want %v (not pid %d)", st, pid, want, notPID)
	return 0
}

func alive(pid uint32) bool { return unix.Kill(int(pid), 0) == nil }

// The official gocore runs directly where the glibc is new enough and through
// the bundled runtime elsewhere; the workflow says which to expect.
func TestLinuxGocoreMode(t *testing.T) {
	p := installedPaths(t)
	argv := gocoreCommand(p)
	t.Logf("gocore command: %q", argv)
	want := os.Getenv("CORE_STARTER_EXPECT_RUNTIME")
	if want == "" {
		t.Skip("CORE_STARTER_EXPECT_RUNTIME not set")
	}
	if got := gocoreViaRuntime; got != (want == "1") {
		t.Fatalf("via runtime = %v, want %s", got, want)
	}
	out, err := exec.Command(argv[0], append(argv[1:], "version")...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Gocore") {
		t.Fatalf("gocore version: %v\n%s", err, out)
	}
}

func TestLinuxNodeStartQueryStop(t *testing.T) {
	p := installedPaths(t)
	np, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, np)
	snap, err := queryStatus(np.ipc, np.pid)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	t.Logf("snapshot: %+v", *snap)
	comm, _ := os.ReadFile("/proc/" + strconv.Itoa(int(np.pid)) + "/comm")
	if c := strings.TrimSpace(string(comm)); c != "gocore" {
		t.Errorf("process name %q, want gocore (users look for it in the system monitor)", c)
	}
	// A pid that is not gocore's must not get an answer through dialNode.
	if _, err := dialNode(np.ipc, np.pid+1); err == nil {
		t.Error("dialNode accepted a socket served by another pid")
	}
	stopCleanly(t, np)
	if exists(ipcPath(p)) {
		t.Log("note: gocore left its IPC socket file behind")
	}
}

// gocore's signal handler comes after its database: a stop right after the
// start must wait for it instead of killing gocore mid-open.
func TestLinuxStopRightAfterStart(t *testing.T) {
	p := installedPaths(t)
	np, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	stopCleanly(t, np)
}

// A tray killed hard leaves gocore running; the next supervisor adopts it,
// via the pid file and, without one, by scanning /proc.
func TestLinuxSupervisorAdoptsOrphan(t *testing.T) {
	p := installedPaths(t)
	orphan, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, orphan)

	// Via the pid file.
	np, err := adoptNode(p)
	if err != nil || np == nil || np.pid != orphan.pid {
		t.Fatalf("adoptNode = %v, %v; want the orphan pid %d", np, err, orphan.pid)
	}
	np.release()
	// Without one: found by scanning /proc, then stopped by the supervisor.
	os.Remove(p.pidFile)
	sup := newSupervisor(p)
	sup.Start()
	if pid := waitState(t, sup, NodeRunning, 0); pid != orphan.pid {
		t.Fatalf("supervisor runs pid %d, want the orphan %d", pid, orphan.pid)
	}
	sup.mu.Lock()
	adopted := sup.proc
	sup.mu.Unlock()
	sup.Shutdown()
	select {
	case <-orphan.exited:
		<-adopted.exited
		// Only in this test the "orphan" is still our child, so both its
		// cmd.Wait and the adopter's Wait4 wait for it; whichever reaps it
		// first gets the exit code, the other -1.
		code := orphan.exitCode()
		if code == -1 {
			code = adopted.exitCode()
		}
		if code != 0 {
			t.Fatalf("orphan exit code %d (cmd.Wait %d, adopter %d)", code, orphan.exitCode(), adopted.exitCode())
		}
	case <-time.After(time.Minute):
		t.Fatal("adopted node still running after Shutdown")
	}
}

func TestLinuxSupervisorRestartsCrashedNode(t *testing.T) {
	p := installedPaths(t)
	sup := newSupervisor(p)
	sup.Start()
	defer sup.Shutdown()
	first := waitState(t, sup, NodeRunning, 0)
	unix.Kill(int(first), unix.SIGKILL)
	second := waitState(t, sup, NodeRunning, first)
	t.Logf("crashed pid %d, restarted as %d", first, second)
}

// Another gocore on our data folder (started by hand from another file) is
// not ours to adopt or stop, and no second node may start next to it.
func TestLinuxForeignNodeBlocks(t *testing.T) {
	p := installedPaths(t)
	copyDir := t.TempDir()
	foreign := filepath.Join(copyDir, "gocore")
	if err := copyFile(p.gocore, foreign); err != nil {
		t.Fatal(err)
	}
	os.Chmod(foreign, 0o755)
	argv := gocoreCommand(p)
	argv[len(argv)-1] = foreign // the binary itself is the last element
	os.MkdirAll(p.data, 0o700)
	cmd := exec.Command(argv[0], append(argv[1:], nodeArgs(p, filepath.Join(copyDir, "f.ipc"), defaultP2PPort+5)...)...)
	cmd.Env = append(os.Environ(), "GODEBUG=netdns=go")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGINT)
		cmd.Wait()
	}()
	deadline := time.Now().Add(2 * time.Minute)
	for !dataDirLocked(p) {
		if time.Now().After(deadline) {
			t.Fatal("foreign gocore never locked the data folder")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if _, _, err := findNode(p); !errors.Is(err, errForeignNode) {
		t.Fatalf("findNode = %v, want errForeignNode", err)
	}
}

func TestLinuxControlSocket(t *testing.T) {
	integration(t)
	testPaths(t)
	lock, ok := lockFile(trayLockName, 0)
	if !ok {
		t.Fatal("first lock failed")
	}
	if _, ok := lockFile(trayLockName, 0); ok {
		t.Fatal("second tray got the lock")
	}
	reloads := 0
	err := serveControl(traySockName, func(r ctlRequest) ctlReply {
		switch r.Cmd {
		case "ping":
			return ctlReply{OK: true, Version: appVersion, Accepting: true}
		case "reload":
			reloads++
			return ctlReply{OK: true}
		}
		return ctlReply{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !trayAccepting() {
		t.Fatal("trayAccepting = false")
	}
	askTrayReload()
	if reloads != 1 {
		t.Fatalf("reloads = %d", reloads)
	}
	lock.Close()
	if !waitTrayGone(time.Second) {
		t.Fatal("lock not free after close")
	}
}

func TestLinuxAutostartFile(t *testing.T) {
	integration(t)
	p := testPaths(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := setAutostart(p, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(autostartFile())
	if !strings.Contains(string(raw), "Exec="+p.exe+" --autostart") || !autostartEnabled(p) {
		t.Fatalf("autostart entry:\n%s", raw)
	}
	// The desktop's own switch wins.
	os.WriteFile(autostartFile(), append(raw, []byte("Hidden=true\n")...), 0o600)
	if autostartEnabled(p) {
		t.Fatal("Hidden=true still counts as enabled")
	}
	if err := setAutostart(p, false); err != nil || exists(autostartFile()) {
		t.Fatalf("off: %v, file left: %v", err, exists(autostartFile()))
	}
}

func display(t *testing.T) {
	t.Helper()
	integration(t)
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display (run under xvfb-run)")
	}
}

// screenshot saves the screen for the CI artifact (looked at by a human).
func screenshot(t *testing.T, name string) {
	dir := os.Getenv("CORE_STARTER_SHOTS")
	if dir == "" {
		return
	}
	if out, err := exec.Command("import", "-window", "root", filepath.Join(dir, name+".png")).CombinedOutput(); err != nil {
		t.Logf("screenshot: %v %s", err, out)
	}
}

// gtkThread runs every window test on one OS thread: GTK belongs to the
// thread that initialised it.
var gtkThread = func() chan func() {
	c := make(chan func())
	go func() {
		runtime.LockOSThread()
		for f := range c {
			f()
		}
	}()
	return c
}()

func windowRun(t *testing.T, p paths, mode uiMode, script string, shot string, onSave func(Settings) error) bool {
	t.Helper()
	uiTestScript = script
	defer func() { uiTestScript = "" }()
	done := make(chan bool, 1)
	gtkThread <- func() { done <- runWindow(textsDE, p, mode, "", onSave) }
	if shot != "" {
		time.Sleep(6 * time.Second)
		screenshot(t, shot)
	}
	select {
	case ok := <-done:
		return ok
	case <-time.After(2 * time.Minute):
		t.Fatal("window did not finish within 2 minutes")
	}
	return false
}

func TestLinuxSettingsWindow(t *testing.T) {
	display(t)
	p := testPaths(t)
	var got Settings
	ok := windowRun(t, p, uiSettings, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(8000); // the screenshot first
		const mine = document.getElementById("mine");
		if (!mine.checked) mine.click();
		const a = document.getElementById("address");
		a.value = "CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963";
		a.dispatchEvent(new Event("input"));
		for (let i = 0; i < 50 && !document.getElementById("addrHint").classList.contains("good"); i++) await wait(100);
		`+jsAgree+`
		document.querySelector('[data-p="75"]').click();
		document.getElementById("save").click();
	})()`, "settings-window", func(s Settings) error {
		if err := s.Normalize(); err != nil {
			return err
		}
		got = s
		return nil
	})
	if !ok {
		t.Fatal("window closed without saving")
	}
	t.Logf("saved: %+v", got)
	if !got.Mine || got.Address != "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963" || got.Power != 75 || !got.Node || !got.nodeConsented() || !got.mineConsented() {
		t.Errorf("unexpected settings %+v", got)
	}
}

// The install window shows the Linux texts, hides "Uninstall" and blocks any
// navigation away from the page.
func TestLinuxInstallWindow(t *testing.T) {
	display(t)
	p := testPaths(t)
	ok := windowRun(t, p, uiInstall, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(8000);
		const intro = document.getElementById("intro") ? document.getElementById("intro").textContent : document.body.textContent;
		if (/Taskleiste|Windows/.test(intro)) return; // Windows texts: no close, the test times out
		location.href = "https://example.com/";
		await wait(1500);
		// Still our page (navigation refused): save closes the window.
		`+jsAgree+`
		document.getElementById("save").click();
	})()`, "install-window", func(s Settings) error {
		if !s.nodeConsented() {
			return errConsent
		}
		return s.Normalize()
	})
	if !ok {
		t.Fatal("window did not save (navigation not blocked, or Windows texts shown)")
	}
}

// A changed mining text: the saved consent is from an older version, so
// switching mining on shows the mining notes again and saving needs the tick.
func TestLinuxConsentAgain(t *testing.T) {
	display(t)
	p := testPaths(t)
	s := Settings{Node: true, Mine: false, Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "pc-test", Power: 50, Lang: "de", Consent: agreedConsent()}
	s.Consent.Mine.Version = consentMineVersion - 1
	if err := SaveSettings(p.settings, s); err != nil {
		t.Fatal(err)
	}
	var got Settings
	ok := windowRun(t, p, uiSettings, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(3000);
		document.getElementById("mine").click();
		await wait(300);
		const list = document.getElementById("consentList").textContent;
		if (!list.includes("0,42") || list.includes("GB Speicher")) return; // mining part only
		`+jsAgree+`
		document.getElementById("save").click();
	})()`, "consent-again", func(s Settings) error {
		got = s
		return s.Normalize()
	})
	if !ok {
		t.Fatal("window did not ask again or did not save")
	}
	if !got.Mine || !got.mineConsented() || !got.Consent.Node.At.Equal(s.Consent.Node.At) {
		t.Errorf("consent after saving: node %+v, mine %+v", got.Consent.Node, got.Consent.Mine)
	}
}

// A page that never draws a frame: the watchdog closes the window and the
// failure handler runs (in the program: restart with safer WebKit modes).
func TestLinuxWindowWatchdog(t *testing.T) {
	display(t)
	p := testPaths(t)
	page, wait, fail := uiPage, uiWatchdog, onWindowFailed
	failed := make(chan struct{}, 1)
	uiPage, uiWatchdog = `<!doctype html><title>x</title><script>chrome.webview.postMessage(JSON.stringify({id: 1, cmd: "init", arg: null}))</script>`, 3*time.Second
	onWindowFailed = func(texts, paths) { failed <- struct{}{} }
	defer func() { uiPage, uiWatchdog, onWindowFailed = page, wait, fail }()
	windowRun(t, p, uiSettings, "", "", func(Settings) error { return nil })
	select {
	case <-failed:
	default:
		t.Fatal("watchdog did not report the failed window")
	}
}
