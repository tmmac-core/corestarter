# Changelog

All notable changes to CoreStarter. Versions follow `appVersion` in `app.go`.

## Unreleased

### Added
- "My network" card in the settings window: a world map with the countries of
  the nodes that answered your node in the last 24 hours, how many are
  connected now, and whether a node has connected to yours from outside
  since it started (if not, how to forward the port safely). Read from the
  node's own address book on this computer; the starter opens no connection
  for it and shows no addresses, only countries.
- Sync progress in the settings window: a thin bar and an estimate of the
  time left ("Step 1 of 2: loading blocks · about 3 h left"), measured from
  the node's own block rate; step 2 (account state) has no total in gocore,
  so it gets no estimate. When the sync finishes with the window open, it
  says so.
- When the node cannot run, the tray and the window name the reason: another
  gocore holds the data folder, no free P2P port, low disk space, or repeated
  crashes. The window offers the log folder (data folder when the disk is
  full) and a button that copies versions and state for support, without
  any address or node ID.

### Changed
- Japanese, Dutch and Romanian texts reviewed against the German: several
  sentences that said something slightly different now say the same (for
  example the support share and the overheating note), plus more natural
  wording and one term per concept.
- NOTICE now also names the parts behind "My network": goleveldb and
  golang/snappy, DB-IP's country table (CC BY 4.0) and the Natural Earth
  map. As with every change to these texts, the node consent asks once
  more after the update (version 5).
- When the pool's statistics (Catch That Rabbit's API) don't answer, the
  miner view says so, adds that mining carries on as normal and when the
  next try is; earlier figures stay with the time they are from. Before, it
  kept saying the earnings were being fetched.
- The installed window opens on a "Status" tab, next to "Miner" (while
  mining is on) and "Settings". Status shows the node, the sync progress, a
  new "Your contribution" card (running time, new blocks your node verified
  since it started, size of its copy of the chain), "My network" and a line
  on the miner. The tray item is now "Status and settings …".
- Settings window: calmer layout on a 4/8 px grid, fewer font sizes, the
  links moved out of the sticky footer (73 instead of 201 px high on a narrow
  window), no sideways scrolling at 375 px, the three info sections grouped
  in one card with full-width rows, a visible switch track when off.
- Motion only on a change of state, with the brand's durations and curves;
  none at rest, none with "reduce motion", none while the window is hidden.
- Linux packages carry glibc 2.39-0ubuntu8.12 (Ubuntu 24.04 security
  update) instead of 8.9.

## 1.1.0

### Added
- Intel Macs: a separate download, `CoreStarter-<version>-mac-intel.dmg`. It
  bundles the official gocore v2.2.1, the last release Core built for Intel
  Macs; v2.2.2 differs only in an RPC parameter the starter does not use, not
  in consensus.
- The Intel version on a Mac with Apple silicon says so before mining: it
  runs translated (Rosetta) and much slower than the Apple silicon version.

## 1.0.2

### Added
- The miner view shows your Core address in groups of four, with a "Copy"
  button. Selecting it by hand also copies it without spaces (reported: the
  address could not be copied to look it up on the pool's page).
- "View in the pool (Catch That Rabbit)" opens the pool's page for your
  address in the browser, for the region holding the most. CoreStarter itself
  still connects nowhere for this.

## 1.0.1

### Fixed
- Windows: the miner opts out of Windows' power throttling. Windows 11 slowed
  it down whenever CoreStarter was not in the foreground (reported: 25 % CPU
  and ~1.2 instead of ~8.3 kH/s on an idle 16-thread PC, full speed only while
  the tray menu was open). It still runs at idle priority and yields to every other
  program. `miner.log` says whether the opt-out worked.

### Added
- `core-miner --bench` on Windows measures two more variants: "Windows
  decides" (the behaviour of 1.0.0) and "efficiency mode".

## 1.0.0

First public release. The programs are not signed yet.

### Added
- The window links the source code (github.com/tmmac-core/corestarter) and,
  once installed, the release page ("Check for a new version") on
  tellmemoreaboutcore.com.
- Every release carries `third-party-sources-<version>.tar`, the unchanged
  source of the bundled gocore (with its Go modules), RandomY, GCC runtime,
  winpthreads and glibc.
- Linux packages ship the full GPL-2, GPL-3 and LGPL-2.1 texts (Fedora and
  openSUSE lack `/usr/share/common-licenses`).

### Changed
- NOTICE names where the source lies and says correctly that the Windows window
  uses go-webview2's Go loader, not Microsoft's WebView2Loader.dll. The node
  consent therefore asks once more (version 4).

## 0.8.3

Release preparation after a full review on Windows, Linux and macOS.

### Fixed
- Windows: the miner program is only unpacked when mining is switched on. An
  antivirus that blocks or quarantines it no longer stops the installation or
  the node.
- Windows: unpacking and copying retry when a virus scanner still holds a fresh
  file.
- Windows, Linux: at logout or shutdown a node that is still opening its
  database gets the stop signal at once instead of after a long wait.
- macOS: an open window no longer cancels logout, restart or shutdown.
- macOS, Linux: a quit request to the background part stops miner and node
  cleanly, like the end of the session.
- macOS: the window reloads itself if its web content process ends, instead of
  staying empty; the title bar is dark like the page.
- Linux: the window no longer crashes a few seconds after opening on openSUSE
  Leap 15.6 (WebKitGTK 2.52.1 fails on scripts over 16 KB; long replies now go
  to the page in pieces).
- Windows, Linux: the window reloads itself (up to three times) if its web
  process ends, instead of staying empty.
- Settings: the language can be set back to "Auto"; a settings file that only
  says `null` no longer breaks saving.
- Miner: rejected shares are summed up per status tick and the pool's reason is
  shortened, so a broken pool cannot flood `miner.log`.

### Changed
- Setup: "Install and start" is never a grey dead end. A click while something
  is missing leads to it (the tick box, the address); the reason is shown next
  to the button. The "Before you start" box sits right after the switches.
- Miner view: calm empty states (no big placeholder, no empty chart while not
  mining), one clear message when the pool cannot be reached, the measured
  support share only after a full 100-minute window.
- Accessibility: switches are announced as switches with their description,
  the miner state is a live region, focus moves with the view; Escape no longer
  closes the window during setup or while typing.
- Texts: download only from tellmemoreaboutcore.com (window and Mac guide);
  the "done" view says the window can be closed.
- Linux build: the pinned gocore is checked to load no library outside the
  bundled runtime.
