//go:build darwin

package main

// Integration tests against the built app (dist/CoreStarter.app from
// scripts/build-mac.sh, or CORE_STARTER_APP). They start an offline node (no
// discovery, no peers, no UPnP) in a temp folder, so they only run when
// CORE_STARTER_DARWIN_IT=1. No macOS CI: run them on a Mac.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// helperMiner is set by darwin_miner_test.go (not in -tags nominer builds).
var helperMiner func()

// mainThread runs functions on the process's main thread: AppKit only works
// there, and Go tests run on other goroutines.
var mainThread = make(chan func())

func TestMain(m *testing.M) {
	if os.Getenv("CORE_STARTER_DARWIN_IT") == "1" {
		extraNodeArgs = []string{"--nodiscover", "--maxpeers", "0", "--nat", "none"}
	}
	if os.Getenv("CORE_STARTER_TEST_HELPER") == "miner" && helperMiner != nil {
		helperMiner() // never returns
	}
	go func() { os.Exit(m.Run()) }()
	for f := range mainThread { // init locked the main goroutine to the main thread
		f()
	}
}

func integration(t *testing.T) {
	t.Helper()
	if os.Getenv("CORE_STARTER_DARWIN_IT") != "1" {
		t.Skip("set CORE_STARTER_DARWIN_IT=1 (starts gocore from the built app)")
	}
}

func builtApp(t *testing.T) string {
	if a := os.Getenv("CORE_STARTER_APP"); a != "" {
		return a
	}
	wd, _ := os.Getwd()
	return filepath.Join(wd, "dist", "CoreStarter.app")
}

func testPaths(t *testing.T) paths {
	t.Helper()
	// Short folder names: the IPC socket path must stay under 104 bytes.
	tmp, err := os.MkdirTemp("/tmp", "cs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	p := pathsAt(filepath.Join(tmp, "CoreStarter"), filepath.Join(tmp, "logs"), builtApp(t))
	p.run = filepath.Join(tmp, "run")
	if err := privateDir(p.run); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("%s missing: run scripts/build-mac.sh first", p.gocore)
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

// Paths: bundle layout, helper copy, run folder and data with spaces.
func TestDarwinPaths(t *testing.T) {
	app := "/Applications/CoreStarter.app"
	for _, exe := range []string{app + "/Contents/MacOS/core-starter", app + "/" + helperApp + "/Contents/MacOS/core-starter"} {
		if b := bundleOf(exe); b != app {
			t.Errorf("bundleOf(%q) = %q", exe, b)
		}
	}
	p := pathsAt("/x/CoreStarter", "/x/logs", app)
	if p.gocore != app+"/Contents/MacOS/gocore" || helperExe(p) != app+"/"+helperApp+"/Contents/MacOS/core-starter" {
		t.Errorf("paths %+v helper %s", p, helperExe(p))
	}
	if !inApplications(app) || inApplications("/Volumes/CoreStarter/CoreStarter.app") ||
		inApplications("/private/var/folders/x/AppTranslocation/y/d/CoreStarter.app") {
		t.Error("inApplications wrong")
	}
	real, err := appPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(ipcPath(real)) >= sunPathMax {
		t.Errorf("IPC path too long: %s", ipcPath(real))
	}
	if strings.Contains(real.run, "/T/") {
		t.Errorf("run folder in TMPDIR (cleaned after 3 days): %s", real.run)
	}
}

// The launchd job: helper path, crash restart, Aqua only.
func TestDarwinJobPlist(t *testing.T) {
	p := pathsAt("/x/CoreStarter", "/x/logs", "/Applications/CoreStarter.app")
	raw, err := jobPlist(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{helperExe(p), "<string>--tray</string>", "<key>SuccessfulExit</key><false/>", "<string>Aqua</string>"} {
		if !strings.Contains(s, want) {
			t.Errorf("plist lacks %q:\n%s", want, s)
		}
	}
	f := filepath.Join(t.TempDir(), "j.plist")
	os.WriteFile(f, raw, 0o600)
	if out, err := exec.Command("/usr/bin/plutil", "-lint", f).CombinedOutput(); err != nil {
		t.Errorf("plutil: %v %s", err, out)
	}
}

// procArgs reads our own arguments back (kern.procargs2).
func TestDarwinProcArgs(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	time.Sleep(200 * time.Millisecond)
	exe, args, err := procArgs(cmd.Process.Pid)
	if err != nil || exe != "/bin/sleep" || len(args) != 2 || args[1] != "30" {
		t.Fatalf("procArgs = %q %q %v", exe, args, err)
	}
	st, err := procStartTime(cmd.Process.Pid)
	if err != nil || st == 0 || !sameProcess(cmd.Process.Pid, st) || !ownProcess(cmd.Process.Pid) {
		t.Fatalf("start time %d %v", st, err)
	}
}

func TestDarwinNodeStartQueryStop(t *testing.T) {
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
	// A pid that is not gocore's must not get an answer through dialNode.
	if _, err := dialNode(np.ipc, np.pid+1); err == nil {
		t.Error("dialNode accepted a socket served by another pid")
	}
	if !exists(filepath.Join(p.data, ".metadata_never_index")) {
		t.Error("data folder not excluded from Spotlight/Time Machine")
	}
	stopCleanly(t, np)
}

// gocore's signal handler comes after its database: a stop right after the
// start must wait for it instead of killing gocore mid-open.
func TestDarwinStopRightAfterStart(t *testing.T) {
	p := installedPaths(t)
	np, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	stopCleanly(t, np)
}

// A helper killed hard leaves gocore running; the next supervisor adopts it,
// via the pid file and, without one, from the process table.
func TestDarwinSupervisorAdoptsOrphan(t *testing.T) {
	p := installedPaths(t)
	orphan, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, orphan)
	np, err := adoptNode(p)
	if err != nil || np == nil || np.pid != orphan.pid {
		t.Fatalf("adoptNode = %v, %v; want the orphan pid %d", np, err, orphan.pid)
	}
	np.release()
	os.Remove(p.pidFile)
	sup := newSupervisor(p)
	sup.Start()
	if pid := waitState(t, sup, NodeRunning, 0); pid != orphan.pid {
		t.Fatalf("supervisor runs pid %d, want the orphan %d", pid, orphan.pid)
	}
	sup.Shutdown()
	select {
	case <-orphan.exited:
	case <-time.After(time.Minute):
		t.Fatal("adopted node still running after Shutdown")
	}
}

// After an app update the running gocore is the old file: the adopter stops
// it so the new one starts.
func TestDarwinAdoptRestartsReplacedNode(t *testing.T) {
	p := installedPaths(t)
	orphan, err := startNode(p, defaultP2PPort)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, orphan)
	raw, _ := os.ReadFile(p.pidFile)
	f := strings.Fields(string(raw))
	os.WriteFile(p.pidFile, []byte(f[0]+" "+f[1]+" 1:1"), 0o600) // another file identity
	if np, err := adoptNode(p); np != nil || err != nil {
		t.Fatalf("adoptNode = %v, %v; want the replaced node stopped", np, err)
	}
	select {
	case <-orphan.exited:
	case <-time.After(time.Minute):
		t.Fatal("replaced node still running")
	}
}

func TestDarwinSupervisorRestartsCrashedNode(t *testing.T) {
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
func TestDarwinForeignNodeBlocks(t *testing.T) {
	p := installedPaths(t)
	copyDir := t.TempDir()
	foreign := filepath.Join(copyDir, "gocore")
	if err := copyFile(p.gocore, foreign); err != nil {
		t.Fatal(err)
	}
	os.Chmod(foreign, 0o755)
	os.MkdirAll(p.data, 0o700)
	cmd := exec.Command(foreign, nodeArgs(p, filepath.Join(copyDir, "f.ipc"), defaultP2PPort+5)...)
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
	if _, _, _, err := findNode(p); !errors.Is(err, errForeignNode) {
		t.Fatalf("findNode = %v, want errForeignNode", err)
	}
}

func TestDarwinControlSocket(t *testing.T) {
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

func windowRun(t *testing.T, p paths, mode uiMode, script string, onSave func(Settings) error) bool {
	t.Helper()
	uiTestScript = script
	defer func() { uiTestScript = "" }()
	done := make(chan bool, 1)
	mainThread <- func() { done <- runWindow(textsDE, p, mode, "", onSave) }
	select {
	case ok := <-done:
		return ok
	case <-time.After(2 * time.Minute):
		t.Fatal("window did not finish within 2 minutes")
	}
	return false
}

func TestDarwinSettingsWindow(t *testing.T) {
	integration(t)
	p := testPaths(t)
	var got Settings
	ok := windowRun(t, p, uiSettings, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(3000);
		const mine = document.getElementById("mine");
		if (!mine.checked) mine.click();
		const a = document.getElementById("address");
		a.value = "CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963";
		a.dispatchEvent(new Event("input"));
		for (let i = 0; i < 50 && !document.getElementById("addrHint").classList.contains("good"); i++) await wait(100);
		`+jsAgree+`
		document.querySelector('[data-p="75"]').click();
		document.getElementById("save").click();
	})()`, func(s Settings) error {
		if err := s.Normalize(); err != nil {
			return err
		}
		got = s
		return nil
	})
	if !ok {
		t.Fatal("window closed without saving")
	}
	if !got.Mine || got.Address != "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963" || got.Power != 75 || !got.Node || !got.nodeConsented() || !got.mineConsented() {
		t.Errorf("unexpected settings %+v", got)
	}
}

// The install window shows the macOS texts and blocks any navigation away
// from the page.
func TestDarwinInstallWindow(t *testing.T) {
	integration(t)
	p := testPaths(t)
	ok := windowRun(t, p, uiInstall, `(async () => {
		const wait = (ms) => new Promise((r) => setTimeout(r, ms));
		await wait(3000);
		const text = document.body.innerText; // visible text only, not the script with every language
		if (/Taskleiste|Startmenü/.test(text) || !/Menüleiste/.test(text)) return; // wrong texts: the test times out
		location.href = "https://example.com/";
		await wait(1500);
		// Still our page (navigation refused): save closes the window.
		`+jsAgree+`
		document.getElementById("save").click();
	})()`, func(s Settings) error {
		if !s.nodeConsented() {
			return errConsent
		}
		return s.Normalize()
	})
	if !ok {
		t.Fatal("window did not save (navigation not blocked, or wrong texts shown)")
	}
}

// A changed mining text: the saved consent is from an older version, so
// switching mining on shows the mining notes again and saving needs the tick.
func TestDarwinConsentAgain(t *testing.T) {
	integration(t)
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
	})()`, func(s Settings) error {
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

// Every language has the macOS texts, and the uninstall question names the
// data folder that stays (not the shared Windows wording).
func TestDarwinTexts(t *testing.T) {
	for lang, tx := range textsByLang {
		x, ok := macTexts[lang]
		if !ok {
			t.Errorf("%s: no macOS texts", lang)
			continue
		}
		for i, s := range x.menu {
			if s == "" {
				t.Errorf("%s: menu label %d empty", lang, i)
			}
		}
		if x.moveToApps == "" || x.uninstallConfirm == "" || x.noRoot == "" || x.crashHint == "" {
			t.Errorf("%s: macOS text missing", lang)
		}
		if !strings.Contains(x.uninstallConfirm, "Application Support") {
			t.Errorf("%s: uninstall question does not say where the data stays", lang)
		}
		if tx.noAdmin != x.noRoot {
			t.Errorf("%s: noAdmin not replaced by the macOS text", lang)
		}
	}
}
