# Changelog

All notable changes to CoreStarter. Versions follow `appVersion` in `app.go`.

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
