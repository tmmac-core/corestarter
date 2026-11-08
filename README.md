# CoreStarter

Run your own [Core Blockchain](https://coreblockchain.net) node with one click, on Windows, Linux or macOS.
If you want, CoreStarter also mines XCB on your computer.

CoreStarter installs the **official, unchanged** Core node (`gocore`), keeps it running in the background and
shows its state in the taskbar or menu bar. Mining is optional and off by default.

CoreStarter is an independent project by [Tell Me More About Core (TMMAC)](https://tellmemoreaboutcore.com),
not an official app of Core or CoDeTech. [Deutsch: README.de.md](README.de.md)

## Download

Get it from the [download page](https://tellmemoreaboutcore.com/core-starter/) or from
[Releases](https://github.com/tmmac-core/corestarter/releases/latest):

| System | File |
|---|---|
| Windows, 64-bit (x64) | `CoreStarter.exe` |
| Ubuntu 22.04+, Linux Mint 21+, Debian 12+ | `core-starter_<version>_amd64.deb` |
| Fedora, openSUSE | `core-starter-<version>-1.x86_64.rpm` |
| Other Linux (x86_64) | `core-starter_<version>_linux_amd64.tar.gz` |
| macOS 15 or newer, Apple silicon (M1 and later) | `CoreStarter-<version>-mac-arm64.dmg` |
| macOS 15 or newer, Intel | `CoreStarter-<version>-mac-intel.dmg` |

Check your download against `SHA256SUMS` from the same release:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing     # macOS, Linux
```
```
certutil -hashfile CoreStarter.exe SHA256          # Windows, compare with SHA256SUMS
```

### Not signed yet

The programs are not signed with a Microsoft or Apple certificate yet, so your system warns the first time:

- **Windows:** SmartScreen shows "Windows protected your PC": click "More info", then "Run anyway".
  With **Smart App Control** switched on, Windows blocks unsigned programs completely; CoreStarter cannot run
  there until it is signed. Antivirus programs may flag the built-in miner as a "coin miner" (potentially
  unwanted application).
- **macOS:** On first open click "Done", then go to System Settings → Privacy & Security → "Open Anyway".
- **Linux:** install the package with your software manager or `sudo apt install ./core-starter_*.deb`
  (`sudo dnf install ./core-starter-*.rpm` on Fedora).

## How it works

1. **Start CoreStarter.** A window explains what will happen. Nothing is installed or written before you tick
   the consent box and click "Install and start".
2. **Choose:** run the node (on), mine XCB (off), start with your computer (off). The window is available in
   English, German, Japanese, Dutch and Romanian.
3. **It runs in the background.** An icon in the taskbar (Windows, Linux) or menu bar (macOS) shows the state of
   node and miner and opens the settings, logs and data folders. If the node crashes, CoreStarter restarts it.
4. **Stop or remove it** from the tray menu or the window ("Uninstall"). Your account keys
   (`keystore`) are never deleted.

### What you need

- About **15 GB free disk space** (the blockchain grows over time) and **2–3 GB of memory**.
- An internet connection. The node talks to other Core nodes; your IP address is visible to them.

### Mining (optional)

- Off by default. When you switch it on, CoreStarter asks for your Core address and how much of your CPU to use
  (25 %, 50 %, 75 % or all), and asks for a second consent.
- The miner computes for the pool [Catch That Rabbit](https://catchthatrabbit.com), which pays XCB to your
  address. CoreStarter never holds your coins.
- **Support share:** 0.42 % of the computing time (25 seconds every 100 minutes) goes to the TMMAC address. It is
  fixed and the same for everyone; this is how the project is funded.
- Before every start the miner checks its own results against known values and does not mine if they are wrong.
  It pauses on battery power, and on a Mac also when macOS reports the Mac is getting too hot.
- No promised earnings: what mining brings depends on your device, the network and the pool.

### Network and privacy

- **CoreStarter itself opens no connection:** no update check, no tracking, no telemetry.
- The **node** connects to the Core peer-to-peer network (port 30300, or the next free one up to 30320), may
  open a router port via UPnP, asks DNS for start nodes and, if your clock is off, a time server (`pool.ntp.org`).
- The **miner**, only while mining is on, connects to the Catch That Rabbit pool (`*.catchthatrabbit.com:8008`)
  and its earnings API. The pool sees your address, worker name, hash rate and IP address.
- The window loads nothing from the internet. Links (privacy policy, source code, new version) open in your
  browser only when you click them.

Privacy policy: [tellmemoreaboutcore.com/privacy/#apps](https://tellmemoreaboutcore.com/privacy/#apps)

### Where your data lives

| | Windows | Linux | macOS |
|---|---|---|---|
| Program | `%LOCALAPPDATA%\CoreStarter` | `/usr/lib/core-starter/` | `/Applications/CoreStarter.app` |
| Blockchain and keys | `%LOCALAPPDATA%\CoreStarter\data` | `~/.local/share/core-starter/data` | `~/Library/Application Support/Core Starter/data` |
| Settings | `%LOCALAPPDATA%\CoreStarter\settings.json` | `~/.config/core-starter/` | `~/Library/Application Support/Core Starter/` |
| Logs | `%LOCALAPPDATA%\CoreStarter\logs` | `~/.local/state/core-starter/` | `~/Library/Logs/Core Starter/` |

## Build from source

```bash
git clone --recurse-submodules https://github.com/tmmac-core/corestarter.git
cd corestarter
./scripts/build.sh                 # Windows exe with miner (needs mingw-w64 and cmake)
NOMINER=1 ./scripts/build.sh       # Windows exe without miner
./scripts/build-mac.sh             # macOS app and DMG (Apple silicon)
MAC_ARCH=x86_64 ./scripts/build-mac.sh  # same for Intel Macs
go test -race . ./internal/...     # platform-independent tests
```

Linux packages are built in an `ubuntu:22.04` container:
`./scripts/linux-build-deps.sh && ./scripts/build-linux.sh`. Every download is pinned by SHA-256 in the build
scripts; `gocore` is always the official release, never rebuilt.

Technical details (process supervision, tests, limits per system), in German:
[docs/TECHNICAL.de.md](docs/TECHNICAL.de.md). Release steps: [docs/RELEASE.md](docs/RELEASE.md).

## Feedback and security

- Bugs and questions: [GitHub Issues](https://github.com/tmmac-core/corestarter/issues) or the
  [contact form](https://tellmemoreaboutcore.com/imprint/#contact).
- Security problems: please report them privately, see [SECURITY.md](SECURITY.md).

## Licence

CoreStarter is free software under the **GPL-3.0** with additional terms under section 7 (attribution, own name
for modified versions): [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), [TRADEMARKS.md](TRADEMARKS.md).
Copyright (C) 2026 TMMAC.

Bundled third-party parts (gocore, RandomY, GCC runtime, winpthreads, glibc, Go modules, fonts) keep their own
licences; their texts ship with the program, and every release includes `third-party-sources-<version>.tar` with
their unchanged source.
