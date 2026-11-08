# CoreStarter

Eine eigene [Core-Blockchain](https://coreblockchain.net)-Node mit einem Klick, auf Windows, Linux oder macOS.
Auf Wunsch mint CoreStarter auch XCB auf deinem Computer.

CoreStarter installiert die **offizielle, unveränderte** Core-Node (`gocore`), hält sie im Hintergrund am Laufen
und zeigt den Stand in der Taskleiste bzw. Menüleiste. Minen ist freiwillig und standardmäßig aus.

Ein unabhängiges Projekt von [Tell Me More About Core (TMMAC)](https://tellmemoreaboutcore.com/de/), keine
offizielle App von Core oder CoDeTech. [English: README.md](README.md)

## Herunterladen

Über die [Download-Seite](https://tellmemoreaboutcore.com/de/core-starter/) oder unter
[Releases](https://github.com/tmmac-core/corestarter/releases/latest):

| System | Datei |
|---|---|
| Windows, 64 Bit (x64) | `CoreStarter.exe` |
| Ubuntu ab 22.04, Linux Mint ab 21, Debian ab 12 | `core-starter_<version>_amd64.deb` |
| Fedora, openSUSE | `core-starter-<version>-1.x86_64.rpm` |
| anderes Linux (x86_64) | `core-starter_<version>_linux_amd64.tar.gz` |
| macOS ab 15, Apple Silicon (ab M1) | `CoreStarter-<version>-mac-arm64.dmg` |
| macOS ab 15, Intel | `CoreStarter-<version>-mac-intel.dmg` |

Download mit `SHA256SUMS` aus demselben Release vergleichen:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing     # macOS, Linux
```
```
certutil -hashfile CoreStarter.exe SHA256          # Windows, mit SHA256SUMS vergleichen
```

### Noch nicht signiert

Die Programme sind noch nicht mit einem Zertifikat von Microsoft oder Apple signiert, deshalb warnt dein System
beim ersten Mal:

- **Windows:** SmartScreen zeigt „Der Computer wurde durch Windows geschützt“: „Weitere Informationen“, dann
  „Trotzdem ausführen“. Mit eingeschalteter **Intelligenter App-Steuerung** (Smart App Control) blockiert Windows
  unsignierte Programme ganz; dort läuft CoreStarter erst mit Signatur. Virenscanner können den eingebauten Miner
  als „Coin-Miner“ (potenziell unerwünschte Anwendung) melden.
- **macOS:** Beim ersten Öffnen „Fertig“ klicken, dann Systemeinstellungen → Datenschutz & Sicherheit →
  „Dennoch öffnen“.
- **Linux:** Paket über die Software-Verwaltung installieren oder mit `sudo apt install ./core-starter_*.deb`
  (Fedora: `sudo dnf install ./core-starter-*.rpm`).

## So funktioniert es

1. **CoreStarter starten.** Ein Fenster erklärt, was passiert. Vor dem Häkchen im Zustimmungskasten und
   „Installieren und starten“ wird nichts installiert oder geschrieben.
2. **Auswählen:** Node betreiben (an), XCB minen (aus), mit dem Computer starten (aus). Das Fenster gibt es auf
   Deutsch, Englisch, Japanisch, Niederländisch und Rumänisch.
3. **Läuft im Hintergrund.** Ein Symbol in der Taskleiste (Windows, Linux) bzw. Menüleiste (macOS) zeigt den
   Stand von Node und Miner und öffnet Einstellungen, Logs und Datenordner. Stürzt die Node ab, startet
   CoreStarter sie neu.
4. **Beenden oder entfernen** über das Symbol oder das Fenster („Deinstallieren“). Deine Konto-Schlüssel
   (`keystore`) werden nie gelöscht.

### Was du brauchst

- Rund **15 GB freien Speicher** (die Blockchain wächst mit der Zeit) und **2–3 GB Arbeitsspeicher**.
- Eine Internetverbindung. Die Node spricht mit anderen Core-Nodes; deine IP-Adresse ist für sie sichtbar.

### Minen (freiwillig)

- Standardmäßig aus. Schaltest du es ein, fragt CoreStarter nach deiner Core-Adresse und wie viel Prozessor es
  nutzen darf (25 %, 50 %, 75 % oder alles), und fragt ein zweites Mal um Zustimmung.
- Der Miner rechnet für den Pool [Catch That Rabbit](https://catchthatrabbit.com), der XCB an deine Adresse
  auszahlt. CoreStarter verwahrt deine Coins nie.
- **Support-Anteil:** 0,42 % der Rechenzeit (25 Sekunden alle 100 Minuten) gehen an die TMMAC-Adresse. Er ist fest
  und für alle gleich; so finanziert sich das Projekt.
- Vor jedem Start prüft der Miner seine eigenen Ergebnisse gegen bekannte Werte und mint nicht, wenn sie falsch
  sind. Im Akkubetrieb macht er Pause, auf dem Mac auch, wenn macOS meldet, dass der Mac zu heiß wird.
- Kein versprochener Ertrag: Was Minen bringt, hängt von deinem Gerät, dem Netz und dem Pool ab.

### Netz und Datenschutz

- **CoreStarter selbst baut keine Verbindung auf:** kein Update-Check, kein Tracking, keine Telemetrie.
- Die **Node** verbindet sich mit dem Peer-to-Peer-Netz von Core (Port 30300, oder der nächste freie bis 30320),
  öffnet eventuell per UPnP einen Router-Port, fragt DNS nach Start-Nodes und, wenn deine Uhr abweicht, einen
  Zeitserver (`pool.ntp.org`).
- Der **Miner** verbindet sich nur bei eingeschaltetem Minen mit dem Pool Catch That Rabbit
  (`*.catchthatrabbit.com:8008`) und dessen Ertrags-Schnittstelle. Der Pool sieht deine Adresse, den Namen im Pool,
  die Hashrate und deine IP-Adresse.
- Das Fenster lädt nichts aus dem Internet. Links (Datenschutz, Quellcode, neue Version) öffnen erst auf deinen
  Klick den Browser.

[Datenschutzerklärung](https://tellmemoreaboutcore.com/de/datenschutz/#apps)

### Wo deine Daten liegen

| | Windows | Linux | macOS |
|---|---|---|---|
| Programm | `%LOCALAPPDATA%\CoreStarter` | `/usr/lib/core-starter/` | `/Applications/CoreStarter.app` |
| Blockchain und Schlüssel | `%LOCALAPPDATA%\CoreStarter\data` | `~/.local/share/core-starter/data` | `~/Library/Application Support/Core Starter/data` |
| Einstellungen | `%LOCALAPPDATA%\CoreStarter\settings.json` | `~/.config/core-starter/` | `~/Library/Application Support/Core Starter/` |
| Logs | `%LOCALAPPDATA%\CoreStarter\logs` | `~/.local/state/core-starter/` | `~/Library/Logs/Core Starter/` |

## Selbst bauen

```bash
git clone --recurse-submodules https://github.com/tmmac-core/corestarter.git
cd corestarter
./scripts/build.sh                 # Windows-Exe mit Miner (braucht mingw-w64 und cmake)
NOMINER=1 ./scripts/build.sh       # Windows-Exe ohne Miner
./scripts/build-mac.sh             # macOS-App und DMG (Apple Silicon)
MAC_ARCH=x86_64 ./scripts/build-mac.sh  # dasselbe für Intel-Macs
go test -race . ./internal/...     # plattformunabhängige Tests
```

Linux-Pakete entstehen in einem `ubuntu:22.04`-Container:
`./scripts/linux-build-deps.sh && ./scripts/build-linux.sh`. Jeder Download ist in den Build-Skripten per SHA-256
festgenagelt; `gocore` ist immer das offizielle Release, nie selbst gebaut.

Technische Beschreibung (Prozessaufsicht, Tests, Grenzen je System): [docs/TECHNICAL.de.md](docs/TECHNICAL.de.md).
Release-Ablauf: [docs/RELEASE.md](docs/RELEASE.md).

## Feedback und Sicherheit

- Fehler und Fragen: [GitHub Issues](https://github.com/tmmac-core/corestarter/issues) oder das
  [Kontaktformular](https://tellmemoreaboutcore.com/de/impressum/#contact).
- Sicherheitslücken bitte vertraulich melden, siehe [SECURITY.md](SECURITY.md).

## Lizenz

CoreStarter ist freie Software unter der **GPL-3.0** mit Zusätzen nach §7 (Namensnennung, eigener Name für
veränderte Fassungen): [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), [TRADEMARKS.md](TRADEMARKS.md).
Copyright (C) 2026 TMMAC.

Mitgelieferte Fremdteile (gocore, RandomY, GCC-Laufzeit, winpthreads, glibc, Go-Module, Schriften) behalten ihre
eigenen Lizenzen; die Texte liegen dem Programm bei, und jedes Release enthält `third-party-sources-<version>.tar`
mit ihrem unveränderten Quellcode.
