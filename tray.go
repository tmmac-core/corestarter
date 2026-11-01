package main

import (
	"embed"
	"encoding/json"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/systray"
)

const pollInterval = 5 * time.Second

// menu holds the tray items once the icon exists.
type menu struct {
	status, peers, miner     *systray.MenuItem
	peersShown, minerShown   bool
	settings, data, logs     *systray.MenuItem
	restart, uninstall, quit *systray.MenuItem
	minerView                *systray.MenuItem
	minerViewShown           bool
	look                     trayLook // icon shown now (onReady starts with lookOff)
}

// The tray symbol is a power symbol around the unchanged Core mark (assets/):
// the stroke shows the
// node (white off, green running, orange failed), the ring the miner (green
// while mining).
type trayLook int

const (
	lookOff trayLook = iota
	lookNode
	lookMiner
	lookNodeMiner
	lookError
	lookErrorMiner
)

//go:embed assets/tray*.ico
var trayICOs embed.FS

// trayICONames: the file of each look (in assets/).
var trayICONames = [...]string{lookOff: "tray.ico", lookNode: "tray-node.ico", lookMiner: "tray-miner.ico",
	lookNodeMiner: "tray-node-miner.ico", lookError: "tray-error.ico", lookErrorMiner: "tray-error-miner.ico"}

// trayIcons: one image per look, in the format the platform's tray takes.
var trayIcons = func() (out [len(trayICONames)][]byte) {
	for look, name := range trayICONames {
		b, err := trayICOs.ReadFile("assets/" + name)
		if err != nil {
			panic(err) // embedded at build time
		}
		out[look] = trayImage(b)
	}
	return out
}()

// settledNode: starting and stopping keep the node part of the icon as it was,
// so a restart or a retry after a crash does not flicker.
func settledNode(last, st NodeState) NodeState {
	if st == NodeStarting || st == NodeStopping {
		return last
	}
	return st
}

// trayLookFor: st is the last settled node state (see settledNode).
func trayLookFor(st NodeState, mining bool) trayLook {
	switch {
	case st == NodeFailed && mining:
		return lookErrorMiner
	case st == NodeFailed:
		return lookError
	case st == NodeRunning && mining:
		return lookNodeMiner
	case st == NodeRunning:
		return lookNode
	case mining:
		return lookMiner
	}
	return lookOff
}

// retitle switches the fixed menu entries to another language.
func (m *menu) retitle(t texts) {
	m.minerView.SetTitle(t.menuMiner)
	m.settings.SetTitle(t.menuSettings)
	m.data.SetTitle(t.menuOpenData)
	m.logs.SetTitle(t.menuOpenLog)
	m.restart.SetTitle(t.menuRestart)
	m.uninstall.SetTitle(t.menuUninstall)
	m.quit.SetTitle(t.menuQuit)
}

// trayTexts: the tray's current language; a reload with another "lang"
// switches it (menu titles at once, status lines on the next tick).
var trayTexts atomic.Pointer[texts]

// statusLoop polls the node, steers the miner throttle, writes status.json for
// the window and updates the menu once it exists.
func statusLoop(p paths, sup *supervisor, miner *minerSup, needSetup, quitting *atomic.Bool, current *atomic.Pointer[menu]) {
	var tr tracker
	var mt minerTrack
	var lastErr string
	warned := false
	iconNode := NodeStopped // node part of the tray icon, see trayLookFor
	for !quitting.Load() {
		t := *trayTexts.Load()
		st, pid, ipc, diskFull := sup.Current()
		var snap *Snapshot
		if st == NodeRunning {
			s, err := queryStatus(ipc, pid)
			if err == nil {
				snap, lastErr = s, ""
			} else if err.Error() != lastErr {
				lastErr = err.Error()
				log.Printf("status query: %v", err)
			}
		}
		ns := tr.update(t, st, pid, snap, diskFull, time.Now())
		nodeOn := st != NodeStopped
		// First sync competes with the miner for the CPU; a node without peers
		// (P2P blocked) does no work and must not hold the miner at 25 %.
		initialSync := st == NodeRunning && snap != nil && snap.Head == 0 && snap.Peers > 0
		switch {
		case snap != nil:
			miner.SetThrottle(initialSync)
		case st == NodeStopped || st == NodeFailed:
			miner.SetThrottle(false) // no node competing for the CPU
		} // otherwise keep the last known state (starting, query errors)

		as := AppStatus{
			Version: appVersion, Gocore: gocoreVersion, Updated: time.Now(), NeedSetup: needSetup.Load(),
			NodeOn: nodeOn, NodeLine: ns.Line1, PeersLine: ns.Line2, Synced: ns.Synced, Syncing: initialSync,
		}
		switch {
		case as.NeedSetup:
			as.NodeLine, as.PeersLine = t.setupNeeded, ""
		case !nodeOn:
			as.NodeLine, as.PeersLine = t.nodeOff, ""
		}
		as.MinerOn, as.Throttled, as.Miner, as.MinerError = miner.View()
		if as.MinerOn {
			as.MinerLine = minerLine(t, as)
		} else if mineAwaitsConsent.Load() {
			as.MineConsent, as.MinerLine = true, t.minerConsent
		}
		as.History, as.LastShare = mt.update(as.MinerOn, as.Miner, as.Updated)
		mining := as.MinerOn && as.MinerError == "" && as.Miner != nil && as.Miner.State == "mining"
		iconNode = settledNode(iconNode, st)
		if b, err := json.Marshal(as); err == nil && !quitting.Load() {
			writeFileAtomic(p.status, b)
		}

		if m := current.Load(); m != nil {
			m.status.SetTitle(as.NodeLine)
			m.peersShown = showLine(m.peers, as.PeersLine, m.peersShown)
			m.minerShown = showLine(m.miner, as.MinerLine, m.minerShown)
			if as.MinerOn != m.minerViewShown {
				if as.MinerOn {
					m.minerView.Show()
				} else {
					m.minerView.Hide()
				}
				m.minerViewShown = as.MinerOn
			}
			if look := trayLookFor(iconNode, mining); look != m.look {
				systray.SetIcon(trayIcons[look])
				m.look = look
			}
			tip := "CoreStarter: " + as.NodeLine
			if as.PeersLine != "" {
				tip += " · " + as.PeersLine
			}
			if mining {
				// First, so a long node line cannot cut it off at 127 characters.
				tip = "CoreStarter: " + t.tipMining(formatInt(t, uint64(as.Miner.Hashrate+0.5))) + " · " + strings.TrimPrefix(tip, "CoreStarter: ")
			}
			systray.SetTooltip(truncateRunes(tip, 127))
		}
		if st == NodeFailed && !warned {
			warned = true
			hint := t.crashHint
			if diskFull {
				hint = t.diskFullHint
			}
			go warnUser(t, hint)
		}
		if st == NodeRunning {
			warned = false
		}
		time.Sleep(pollInterval)
	}
}

func showLine(item *systray.MenuItem, text string, shown bool) bool {
	if text == "" {
		if shown {
			item.Hide()
		}
		return false
	}
	item.SetTitle(text)
	if !shown {
		item.Show()
	}
	return true
}

func minerLine(t texts, as AppStatus) string {
	if as.MinerError != "" {
		return t.minerFailed(as.MinerError)
	}
	ms := as.Miner
	if ms == nil {
		return t.minerStarting
	}
	switch ms.State {
	case "starting":
		return t.minerStarting
	case "connecting":
		if ms.PoolUnreachable() { // not a short reconnect: something keeps the pool away
			return t.minerOffline(int(ms.OfflineSecs / 60))
		}
		return t.minerConnecting
	case "paused-battery":
		return t.minerPaused
	case "paused-heat":
		return t.minerHot
	case "error":
		return t.minerFailed("") // the miner's own text is English; it is in miner.log
	}
	return t.minerRunning(formatInt(t, uint64(ms.Hashrate+0.5)), ms.Accepted, as.Throttled)
}
