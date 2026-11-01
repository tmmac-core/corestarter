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

Download prüfen: `shasum -a 256 -c SHA256SUMS --ignore-missing` (macOS, Linux), unter Windows in der PowerShell
`Get-FileHash CoreStarter.exe` und mit `SHA256SUMS` vergleichen.

**Noch nicht signiert:**
- **Windows:** SmartScreen zeigt „Der Computer wurde durch Windows geschützt“: „Weitere Informationen“, dann
  „Trotzdem ausführen“. Mit eingeschalteter **Intelligenter App-Steuerung** (Smart App Control) blockiert Windows
  unsignierte Programme ganz; dort läuft CoreStarter erst mit Signatur. Virenscanner können den eingebauten Miner
  als „Coin-Miner“ (potenziell unerwünscht) melden.
- **macOS:** Beim ersten Öffnen „Fertig“, dann Systemeinstellungen → Datenschutz & Sicherheit → „Dennoch öffnen“.

## So funktioniert es

1. **CoreStarter starten.** Ein Fenster erklärt, was passiert. Vor dem Häkchen und „Installieren und starten“
   wird nichts installiert oder geschrieben.
2. **Auswählen:** Node betreiben (an), XCB minen (aus), mit dem Computer starten (aus).
3. **Läuft im Hintergrund.** Ein Symbol zeigt den Stand von Node und Miner. Stürzt die Node ab, startet
   CoreStarter sie neu.
4. **Beenden oder entfernen** über das Symbol oder das Fenster. Deine Konto-Schlüssel (`keystore`) werden nie
   gelöscht.

**Du brauchst** rund 15 GB freien Speicher (wächst mit der Zeit), 2–3 GB Arbeitsspeicher und Internet. Die Node
spricht mit anderen Core-Nodes, deine IP-Adresse ist für sie sichtbar.

**Minen (freiwillig):** Der Miner rechnet für den Pool Catch That Rabbit, der XCB an deine Adresse auszahlt.
0,42 % der Rechenzeit (25 Sekunden alle 100 Minuten) gehen fest an die TMMAC-Adresse, so finanziert sich das
Projekt. Kein versprochener Ertrag.

**Netz und Datenschutz:** CoreStarter selbst baut keine Verbindung auf: kein Update-Check, kein Tracking. Die Node
spricht mit dem Core-Netz, der Miner nur bei eingeschaltetem Minen mit dem Pool.
[Datenschutzerklärung](https://tellmemoreaboutcore.com/de/datenschutz/#apps)

Technische Beschreibung: [docs/TECHNICAL.de.md](docs/TECHNICAL.de.md).

## Lizenz

GPL-3.0 mit Zusätzen nach §7 (Namensnennung, eigener Name für veränderte Fassungen): [LICENSE](LICENSE),
[NOTICE.md](NOTICE.md), [TRADEMARKS.md](TRADEMARKS.md). Copyright (C) 2026 TMMAC.
