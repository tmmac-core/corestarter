# CoreStarter (Windows, Linux und macOS)

Windows: eine einzige `CoreStarter.exe`: doppelklicken, im Startfenster wählen, „Installieren und starten“. Danach läuft im Hintergrund eine Core-Blockchain-Node, auf Wunsch dazu ein XCB-Miner. Ein Symbol in der Taskleiste zeigt den Stand. Linux: ein `.deb`/`.rpm`-Paket mit demselben Fenster und derselben Logik, siehe [Linux](#linux). macOS (Apple Silicon): eine App in einer DMG, siehe [macOS](#macos).

## Herunterladen

Fertige Programme gibt es auf [tellmemoreaboutcore.com/core-starter](https://tellmemoreaboutcore.com/core-starter/) und unter [Releases](https://github.com/tmmac-core/corestarter/releases), jeweils mit SHA-256 in `SHA256SUMS`. Die Programme sind noch nicht signiert: Windows SmartScreen warnt beim ersten Start („Weitere Informationen“ → „Trotzdem ausführen“), mit eingeschaltetem „Smart App Control“ startet die Exe gar nicht; macOS fragt beim ersten Öffnen nach (Systemeinstellungen → Datenschutz & Sicherheit → „Dennoch öffnen“). CoreStarter ist ein unabhängiges Projekt von Tell Me More About Core (TMMAC), keine offizielle App von Core. Lizenz: GPL-3.0 mit Zusätzen nach §7 (`LICENSE`, `NOTICE.md`), Marken: `TRADEMARKS.md`.

## Was die Exe macht

1. **Startfenster** (WebView2, TMMAC-Brand-Design, Deutsch, Englisch, Japanisch, Niederländisch, Rumänisch):
   - Schalter „Node betreiben“ (an), „XCB minen“ (aus), „Mit Windows starten“ (aus).
   - Für das Minen: Core-Adresse (die Prüfziffern werden geprüft, nur Mainnet), Name im Pool, Leistung 25/50/75 % oder „Alle“, Support-Anteil für TMMAC: fest 0,42 %, nicht abschaltbar (wie im Core Mobile Miner).
   - Netzverbindungen und Grenzen stehen im Fenster.
   - **Vor „Installieren und starten“ wird nichts geschrieben.** Ohne gespeicherte Einstellungen startet der Starter weder Node noch Miner.
   - **Zustimmung:** Die Karte „Bevor du startest“ nennt vor der Installation Beta-Status, Lizenz und Haftung (NOTICE, im Fenster aufklappbar), Speicherbedarf, P2P/UPnP mit sichtbarer IP und den Autostart (auch, wenn Autostart später eingeschaltet wird), beim ersten Einschalten des Minens zusätzlich die Last (gewählte Leistung, bei „Alle“ Volllast) und Stromkosten, den festen Support-Anteil, schwankende Erträge und Steuern sowie die Daten, die der Pool sieht. „Installieren und starten“ bzw. „Speichern“ bleiben gesperrt, bis das Häkchen gesetzt ist. `settings.json` hält je Teil die angenommene Textversion und den Zeitpunkt (`consent.node`, `consent.mine`), nur lokal. Ändert sich ein Text, steigt seine Version (`consent.go`) und das Fenster fragt erneut; ohne aktuelle Mining-Zustimmung startet die Taskleiste keinen Miner und sagt das.
   - Fehlt die WebView2-Runtime, erscheint der frühere Bestätigungsdialog. Minen gibt es dann nicht.
2. **Installiert pro Benutzer** nach `%LOCALAPPDATA%\CoreStarter`, ohne Administratorrechte:
   - `CoreStarter.exe` ist die installierte Kopie. Die heruntergeladene Datei darf danach gelöscht werden.
   - `bin\` enthält:
     - die **unveränderte offizielle** `gocore.exe` (SHA-256 wie im go-core-Release),
     - die MinGW-Laufzeit-DLLs, die das Release braucht, aber nicht mitliefert,
     - `core-miner.exe`,
     - die Lizenztexte.
   - `data\` enthält die Blockchain, `logs\` die Logs (`gocore.log`, `miner.log`, `starter.log`).
   - `settings.json` hält die Auswahl, `status.json` den aktuellen Stand für das Fenster.
   - Dazu kommen Autostart (HKCU Run, nur wenn eingeschaltet; Standard aus), eine Startmenü-Verknüpfung und ein Eintrag unter „Apps & Features“.
3. **Node:**
   - Befehl: `gocore --datadir <data> --syncmode fast --cache 1024 --ipcpath <zufällig pro Start>`.
   - Die Node läuft ohne Fenster und mit niedrigerer Priorität. Sie wird sauber per CTRL_BREAK beendet, aber erst, wenn gocore seinen Signal-Handler eingerichtet hat.
   - Status per JSON-RPC über die Named Pipe. Eine Antwort zählt nur, wenn die Prozess-ID des Pipe-Servers stimmt.
4. **Miner** (`core-miner.exe`, eigener Prozess):
   - **Rechenweg:** RandomY (core-coin/RandomY, Submodul, Commit gepinnt), gebaut mit MinGW und statisch gelinkt. Gerechnet wird wie bei coreminer: SHA3-512(Header ‖ Nonce) → RandomY.
   - **Selbsttest vor jedem Start**, je Rechen-Thread, mit 5 Werten. Zwei davon sind echte, vom Pool angenommene Shares. Rechnet der JIT falsch, wechselt der Miner zum Interpreter. Rechnet auch der falsch, wird nicht gemint.
   - **Pool:** Catch That Rabbit über ETHPROXY. Beim Start wird der schnellste Server die Heimat für den ganzen Lauf. Fällt er aus, weicht der Miner aus und kehrt danach zurück.
   - **Support-Anteil:** 25 Sekunden je 100 Minuten für die TMMAC-Adresse, an zufälliger Stelle; fest, nicht abschaltbar.
   - **Last:** „Alle“ = alle logischen Prozessoren (RandomY braucht nur 128 KiB Scratchpad pro Thread, Hyperthreading lohnt sich), Prozesspriorität „Leerlauf“, Pause im Akkubetrieb. Solange die Node zum ersten Mal synchronisiert, höchstens 25 %.
   - **Ertrag:** per CTR-API, summiert über alle Regionen.
   - **Bindung an den Starter:** Der Miner hängt per Job Object am Starter. Endet der Starter, endet auch der Miner, und der nächste Start beginnt frisch. Sitzungen werden nicht übernommen.
5. **Taskleiste:** Status von Node und Miner, „Einstellungen …“ (öffnet das Fenster), „Miner anzeigen …“ (solange Minen an ist), Daten-Ordner, Log-Ordner, Node neu starten, Deinstallieren, Beenden. Ein Klick im Startmenü öffnet bei laufendem Starter die Einstellungen. Das Symbol ist ein Ein-Schalter um das unveränderte Core-Zeichen: Der Strich zeigt die Node (weiß aus, grün läuft, orange gescheitert), der Ring den Miner (grün, solange er rechnet); die Beschreibung nennt die H/s. Die fertigen Bilder liegen in `assets/` (ICO, ICNS, PNG).
   - **Miner-Ansicht:** Zustand, Hashrate mit Verlauf der letzten 10 Minuten, Threads, Leistung je Thread als Raster (ein deutlich langsamerer Thread wird gelb), Share-Schwierigkeit mit erwartetem Abstand, Antwortzeit des Pools, Shares, letzter Share, Pool-Server, Ertrag, Support und das mitlaufende `miner.log` wie in einem Terminal. Sie öffnet sich nach dem Speichern, wenn Minen gerade eingeschaltet wurde, und über das Menü. Schließen beendet den Miner nicht. Der Miner schreibt dafür jeden angenommenen Share und einmal pro Minute eine Zeile mit Hashrate und Zählern ins Log.
6. **Robustheit:**
   - **Abstürze:** Stürzt die Node ab, startet der Starter sie neu. Nach 5 Abstürzen in 10 Minuten wartet er 30 Minuten. Ist der Speicher voll, sagt er das.
   - **Miner:** Er wird bei Abstürzen ebenso neu gestartet. Ein fehlgeschlagener Selbsttest oder eine vom Pool abgelehnte Adresse führt dagegen zu einem dauerhaften Fehler bis zur nächsten Einstellungsänderung.
   - **Abmelden und Herunterfahren:** Der Starter stoppt Miner und Node selbst. Wird das Herunterfahren abgebrochen, startet er sich neu.
   - **Starter abgeschossen:** Die Node läuft weiter und wird beim nächsten Start übernommen. Es entsteht nie eine zweite Node.
   - **Update und Deinstallation:** Eine neue Exe oder die Deinstallation über „Apps & Features“ beenden eine laufende Instanz sauber. Eine ältere Version überschreibt eine neuere nicht.
   - **Konto-Schlüssel:** `data\keystore` wird nie gelöscht.

**Netzverbindungen:**
- Der Starter selbst baut keine Netzverbindung auf, also keinen Update-Check und keine Telemetrie.
- Die **Node** spricht P2P (Port 30300). Sie öffnet per UPnP einen Router-Port (gocore-Standard `--nat any`), fragt DNS-Bootnodes und bei Uhr-Abweichungen einen Zeitserver (`pool.ntp.org`, in gocore enthalten). Ist Port 30300 belegt, nimmt der Starter den nächsten freien (bis 30320).
- Der **Miner** verbindet sich, nur wenn Minen an ist, mit Catch That Rabbit: Pool `*.catchthatrabbit.com:8008`, unverschlüsselt, und die Ertrags-API `https://*-api.catchthatrabbit.com`. Der Pool sieht Adresse, Name im Pool, Hashrate und IP. Die Ertrags-API wird alle 10 Minuten in allen vier Regionen mit der Adresse abgefragt. Für den festen Support-Anteil meldet sich etwa alle 100 Minuten für 25 s eine zweite Pool-Verbindung mit der TMMAC-Adresse von derselben IP an; Pool und Mitlesende im Netz sehen also, dass diese IP CoreStarter nutzt. Für die HTTPS-Prüfung kann Windows selbst Zertifikatsdienste von Microsoft fragen.
- Zeigt das Fenster nichts an (keine gezeichnete Seite nach 45 s, oder WebView2 startet nicht), fällt der Starter auf den einfachen Dialog zurück und legt technische Angaben (WebView2-Version, Fenstergrößen, DPI, Ereignisse) in `%TEMP%\CoreStarter-diag.txt` ab. Nur in diesem Fehlerfall, die Taskleiste löscht die Datei beim nächsten Start.
- Der Link „Datenschutz“ im Fenster öffnet auf Klick den Browser mit `tellmemoreaboutcore.com/privacy/#apps` (Deutsch `/de/datenschutz/#apps`); der Starter selbst verbindet sich dabei nicht.
- Das **Fenster** lädt nichts aus dem Netz (CSP `default-src 'none'`), nimmt keine hineingezogenen Dateien oder Links an und beantwortet nur Nachrichten der eigenen Seite. In einer einmaligen Messung (Spike, 24 s) hat die WebView2-Runtime keine TCP-Verbindung nach außen geöffnet. Was die Runtime nach Edge-Richtlinien selbst sendet, liegt bei Microsoft.

## Bauen

Die Release-Exe baut das CI (GitHub Actions, `ubuntu-24.04` mit `mingw-w64`) und testet sie auf `windows-2025`. Lokal:

```
git submodule update --init       # third_party/RandomY
./scripts/build.sh                # mit Miner, braucht x86_64-w64-mingw32-gcc-posix + cmake
NOMINER=1 ./scripts/build.sh      # ohne Miner (z. B. Mac ohne MinGW), -tags nominer
go test -race . ./internal/...    # plattformunabhängige Tests
GOOS=windows go vet . && GOOS=windows go vet -tags nominer .
```

Miner-Tests mit nativ gebautem RandomY (Mac/Linux):

```
cmake -S third_party/RandomY -B .cache/randomy-native -DCMAKE_BUILD_TYPE=Release
cmake --build .cache/randomy-native --target randomx
CGO_LDFLAGS="-L$PWD/.cache/randomy-native" go test ./cmd/core-miner
```

Was `scripts/build.sh` tut:
- Es lädt gocore samt Lizenz und die MSYS2-UCRT64-Pakete und prüft jede Datei gegen die festgenagelte SHA-256.
- Es baut `core-miner.exe` (RandomY per CMake/MinGW, Go per cgo, statisch).
- Es prüft mit `tools/peimports` jede DLL-Abhängigkeit bis auf Ebene der einzelnen Funktion. Der Miner darf nur Windows-System-DLLs brauchen.
- Es erzeugt `payload/*.gz` und `payload_gen_windows.go` (beides nicht im Git).
- Es bettet Icon und Manifest ein (go-winres).

Neues gocore-Release: `GOCORE_VERSION`, `GOCORE_SHA` und `GOCORE_LICENSE_SHA` in `scripts/build.sh` anpassen.

## Tests im CI

- **Plattformunabhängig:**
  - Statuslogik, RPC-Parser, Einstellungen
  - Miner-Kern: ICAN-Adresse, Stratum-Client gegen einen Fake-Pool, Failover und Rückkehr zum Heimat-Server, Support-Anteil, CTR-API mit gespeicherten Antworten
- **Miner auf Linux und Windows:**
  - Selbsttest
  - Mining gegen einen Pool auf Loopback, der jeden Share mit RandomY nachrechnet
  - Leistungswechsel, Stopp, abgelehnte Adresse
- **Windows-Integration:**
  - echte gocore offline: Start, Status, CTRL_BREAK-Stopp, Stopp direkt nach dem Start, Übernahme, Neustart nach Absturz, offizielle Exe ohne DLLs (`0xC0000135`)
  - Miner-Aufsicht mit der echten `core-miner.exe`
  - das echte Einstellungsfenster: Die Seite wird per Skript ausgefüllt und speichert
  - das Startfenster im Installations-Modus, auch unter einem bildschirmfüllenden Overlay; ein Pixel-Test prüft, dass die Seite wirklich gezeichnet ist
  - die Miner-Ansicht mit vorgegebenem Status und Log
  - Registry, Verknüpfung, Keystore bleibt

## Grenzen

- **SmartScreen und Smart App Control:** Die Exe ist unsigniert. SmartScreen warnt beim ersten Start („Weitere Informationen“ → „Trotzdem ausführen“). Smart App Control blockiert unsignierte Programme komplett, auch die offizielle gocore.exe.
- **Defender:** Er kann Miner-Code als „CoinMiner“ (PUA) einstufen, auch am Verhalten. gocore.exe enthält ebenfalls RandomY. Das ist ungetestet. Der Ausweg ist `NOMINER=1` für eine getrennte Node-Exe, die eigentliche Lösung die Code-Signatur.
- **Ressourcen:** Nur Windows x64. Die Node braucht rund 15 GB freien Speicher und geschätzt 2–3 GB RAM. Der Miner braucht im Vollmodus rund 300 MB plus 2 MB je Thread und wechselt bei unter 1,5 GB freiem RAM in den langsameren Light-Modus.
- **Platz im Betrieb:** Fällt der freie Speicher unter 1 GB, stoppt der Starter die Node sauber und meldet „Speicher voll“; gestartet wird sie erst wieder ab 2 GB frei.
- **Administrator:** Als Administrator gestartet, verweigert sich der Starter (sonst liefen Node und Miner erhöht oder im Profil eines anderen Kontos).
- **Windows auf ARM:** Läuft in der x64-Emulation; der Miner ist dort vermutlich sehr langsam (ungetestet).
- **Roaming-Profile:** Autostart, Startmenü und „Apps & Features“ wandern mit, das Programm in `%LOCALAPPDATA%` nicht. Auf anderen PCs der Domäne bleiben dann tote Einträge.
- **Hitze:** Windows liefert ohne Treiber keine CPU-Temperatur. Der Miner hat deshalb keinen eigenen Hitzeschutz.
- **`$` im Benutzerpfad:** gocore setzt in `--datadir` Umgebungsvariablen ein. Enthält der Pfad ein `$`, verweigert der Starter die Installation.

## Lizenzen

- **CoreStarter:** GPL-3.0 mit Zusätzen nach §7 (`LICENSE`, `NOTICE.md`), Marken: `TRADEMARKS.md`.
- **gocore:** Die LICENSE-Datei von go-core nennt die CORE License v1.0, die Dateiköpfe GPL-3.0-or-later (Programm) und LGPL-3.0-or-later (Bibliothek); alle drei Texte liegen bei.
- **RandomY:** BSD-3-Clause.
- **GCC-Laufzeit:** GPL-3.0 mit GCC Runtime Library Exception; **winpthreads** und **MinGW-w64-Laufzeit** (im Miner): MIT-artig bzw. gemeinfrei.
- **Linux:** glibc (LGPL-2.1) und GCC-Laufzeit für ältere Distributionen.
- **Go-Module** (BSD, MIT, Apache-2.0, ISC) und **Schriften** (SIL OFL 1.1): `LICENSE-go*.txt`, `LICENSE-font-*.txt`.

Der Quellcode aller mitgelieferten Fremdteile (gocore mit allen Go-Modulen, RandomY, GCC-Laufzeit, winpthreads, glibc) liegt jedem Release als `third-party-sources-<version>.tar` bei.

Alle Texte liegen in `bin\` (Linux `/usr/share/doc/core-starter/`, Mac im App-Bundle unter `Contents/Resources/licenses/`), der Knopf „Lizenzen“ im Fenster öffnet den Ordner.

**CoreStarter selbst:** GPL-3.0 mit Zusätzen nach §7 (Namensnennung, eigener Name für Nachbauten, Haftung), wie CorePulse; Herausgeber Tell Me More About Core. Siehe `LICENSE`, `NOTICE.md` und `TRADEMARKS.md`. Copyright (C) 2026 TMMAC.

## Linux

Pakete für x86_64: `.deb` (Ubuntu ab 22.04, Mint ab 21, Debian ab 12), `.rpm` (Fedora, openSUSE), `.tar.gz` (Grundlage für ein AUR-Paket).

**Ablauf:**
- Installation über die Software-Verwaltung (Doppelklick auf das Paket) oder `sudo apt install ./core-starter_*.deb`. Das Paket legt das Programm nach `/usr/lib/core-starter/`, einen Menüeintrag „CoreStarter“ und eine systemd-User-Unit an. Es schreibt nichts ins Benutzerkonto.
- Der erste Start aus dem Menü zeigt dasselbe Fenster wie unter Windows. Vor „Starten“ wird nichts geschrieben.
- Danach läuft der Hintergrundteil (`core-starter --tray`) als systemd-User-Unit `core-starter.service`, ohne systemd direkt. Er überwacht Node und Miner und zeigt, wo die Oberfläche es kann, ein Symbol im Systembereich. Reines GNOME zeigt ohne Erweiterung keine solchen Symbole; dann ist das Fenster (Menüeintrag) die Steuerung.
- „Beim Anmelden starten“ legt `~/.config/autostart/core-starter.desktop` an (Standard: aus).
- Entfernen über die Software-Verwaltung oder `sudo apt remove core-starter` / `sudo dnf remove core-starter`. Die Daten im Benutzerkonto bleiben.

**Pfade (XDG):** Einstellungen `~/.config/core-starter/settings.json`, Zustand und Logs `~/.local/state/core-starter/`, Blockchain `~/.local/share/core-starter/data/` (Keystore darin), Sockets und Locks in `$XDG_RUNTIME_DIR/core-starter/`.

**gocore:** das offizielle Release, unverändert. Es verlangt glibc 2.38 (gebaut auf Ubuntu 24.04). Auf älteren Systemen (Ubuntu 22.04, Debian 12) startet der Starter es über eine mitgelieferte glibc-2.39-Laufzeit aus Ubuntu-Paketen (`runtime/`, Hashes in `scripts/build-linux.sh`); wo die System-glibc reicht, direkt. `GODEBUG=netdns=go`, damit gocore keine NSS-Module des Systems lädt.

**Abmelden, Herunterfahren, Updates:** systemd schickt SIGTERM; der Miner endet sofort, gocore bekommt bis zu 300 s (Unit) zum sauberen Stopp. Stirbt der Hintergrundteil, endet der Miner mit ihm (Pdeathsig, stdin-EOF); gocore läuft weiter und wird beim nächsten Start übernommen (Pidfile mit Startzeit, sonst Suche in `/proc`; eine fremde gocore auf demselben Datenordner wird am Lock erkannt). Nach einem Paket-Update startet sich der Hintergrundteil selbst neu, die Node läuft dabei durch.

**Netz:** wie unter Windows. Fedora und openSUSE blockieren mit firewalld eingehende P2P-Verbindungen (Port 30300); die Node synchronisiert trotzdem, hilft dem Netz aber weniger. Der Starter ändert die Firewall nicht (bräuchte root).

**Fenster:** eigener schmaler Wrapper um WebKitGTK 4.1 (`webview_linux.c`): nur die eingebettete Seite, keine Navigation, kein Kontextmenü, keine Entwicklerwerkzeuge, nichts auf der Platte (flüchtiger Kontext). Bleibt die Seite 45 s ungezeichnet (bekannt bei NVIDIA-Treibern und VMs ohne 3D), startet das Fenster mit WebKits sichereren Zeichenmodi neu.

**Bauen:** im Container `ubuntu:22.04` (älteste unterstützte glibc): `./scripts/linux-build-deps.sh && ./scripts/build-linux.sh`. `tools/elfimports` prüft, dass Starter und Miner nichts Neueres als glibc 2.35 und nur bekannte Bibliotheken brauchen.

**Tests im CI (`linux.yml`):** Bau in `ubuntu:22.04`, dann das echte Paket in Ubuntu 22.04/24.04, Debian 12/13, Fedora 42 und openSUSE Leap 15.6 installieren und als normaler Benutzer testen: gocore offline (direkt bzw. über die Laufzeit), Start/Status/Stopp, Übernahme mit und ohne Pidfile, Absturz-Neustart, fremde Node, Steuer-Socket, Autostart, Miner-Aufsicht gegen einen Pool auf Loopback, Miner stirbt mit dem Hintergrundteil, Fenster unter Xvfb (Speichern, Navigationssperre, Watchdog, Bildschirmfotos als Artefakt). Im CI läuft WebKit ohne seine eigene Sandbox (der Runner erlaubt keine User-Namespaces im Container); der Test auf einem echten Desktop läuft mit Sandbox.

**Dieselben Tests ohne GitHub:** `scripts/ci-local.sh` auf einem amd64-Linux mit Docker baut in denselben Containern, prüft alle sechs Distributionen und testet die systemd-Unit und die gepinnte Laufzeit (`build`, `distros`, `systemd`, `pins` oder alles). Logs und Bildschirmfotos landen in `dist/ci-local/`.

**Grenzen:** nur x86_64 (ARM: Plan L4). Unsigniertes Paket. Kein Paket-Repository, also keine automatischen Updates. Ohne Display (Server) gibt es noch keinen Modus ohne Fenster (Plan L4).


## macOS

Eine DMG für Apple Silicon (M1 und neuer), macOS 15 oder neuer, weil die offizielle gocore nur für arm64 ab macOS 15 erscheint.

**Ablauf:**
- DMG öffnen, „CoreStarter“ in den Ordner Programme ziehen, dort öffnen. Aus der DMG oder aus Downloads gestartet, richtet die App nichts ein: Sie sagt das und öffnet beide Ordner.
- Der erste Start zeigt dasselbe Fenster wie unter Windows. Vor „Starten“ wird nichts geschrieben.
- Danach läuft der Hintergrundteil als eigene kleine App im Bundle (`Contents/Library/LoginItems/CoreStarter Helper.app`, nur Menüleisten-Symbol, kein Dock-Symbol) unter einem launchd-Job des Benutzers. launchd startet ihn nach einem Absturz neu, nicht nach „Beenden“. Mit „Beim Anmelden starten“ liegt der Job in `~/Library/LaunchAgents/info.tmmac.core-starter.plist`, sonst nur für diese Sitzung.
- Fenster schließen (Cmd+W) lässt Node und Miner weiterlaufen. Das Fenster öffnet die App erneut (Programme, Launchpad, Menüleiste).
- **Update:** neue App über die alte ziehen. Der Hintergrundteil merkt den Austausch, startet sich neu und übernimmt die laufende Node; wurde gocore selbst ersetzt, startet er auch die Node neu.
- **Deinstallieren** (Fenster, Karte „Aufhören“): stoppt alles, entfernt den launchd-Job und legt die App in den Papierkorb. Wer die App nur in den Papierkorb zieht: Nach 2 Minuten ohne Programmdatei stoppt der Hintergrundteil alles und entfernt seinen Job. Die Daten bleiben in beiden Fällen.

**Pfade:** Einstellungen, Status und Blockchain in `~/Library/Application Support/Core Starter/` (`data/` ist von Time Machine und Spotlight ausgenommen), Logs in `~/Library/Logs/Core Starter/`, Sockets und Locks im Benutzer-Cache-Ordner (`getconf DARWIN_USER_CACHE_DIR`), weil macOS `$TMPDIR` nach drei Tagen räumt.

**Node und Miner:** gocore bit-genau aus dem offiziellen Release (SHA-256 in `scripts/build-mac.sh`), nice 10, Stopp per SIGINT. Der Miner rechnet je nach Leistung bis auf allen Kernen, mit QoS „utility“ und nice 19, pausiert im Akkubetrieb und wenn macOS hohe Hitze meldet (MacBook Air ohne Lüfter), und startet nach dem Ruhezustand mit frischer Pool-Verbindung. Auf Apple Silicon braucht RandomY den „secure JIT“-Modus (`appleJIT` in `cmd/core-miner/randomy.go`), sonst stürzt der Miner ab.

**Bauen** (auf einem Mac mit Apple Silicon, Command Line Tools, Go aus `go.mod`, cmake): `git submodule update --init && ./scripts/build-mac.sh` → `dist/CoreStarter.app` und `dist/CoreStarter-<version>-mac-arm64.dmg`. `NOMINER=1` ohne Miner, `E2E=1` eine Testvariante (Node offline, Miner gegen `go run ./tools/fakepool` auf 127.0.0.1:47999), die nie weitergegeben wird.

**Tests** (lokal, keine macOS-CI): `go test -race . ./internal/...`, Miner mit `CGO_LDFLAGS=-L$PWD/.cache/mac/randomy go test ./cmd/core-miner`, dazu nach dem Bauen `CORE_STARTER_DARWIN_IT=1 go test -run TestDarwin -v .` gegen die gebaute App (echte gocore offline, Übernahme, Absturz-Neustart, Steuer-Socket, beide Fenster, Miner-Aufsicht gegen einen Pool auf Loopback).

**Grenzen:**
- **Signatur:** Der Entwicklungs-Build ist nur ad hoc signiert. Ab macOS 15 lässt Gatekeeper ihn nach einem Download nur über Systemeinstellungen → Datenschutz & Sicherheit → „Dennoch öffnen“ zu. Eine Developer-ID mit Notarisierung folgt.
- **Lokales Netzwerk:** gocore fragt per UPnP den Router nach einem offenen Port, macOS fragt deshalb beim ersten Mal nach „Geräten im lokalen Netzwerk“. Ablehnen ist in Ordnung, die Node synchronisiert dann mit weniger Verbindungen. Ist die macOS-Firewall an, fragt sie bei eingehenden Verbindungen nach gocore.
- **Ruhezustand:** Deckel zu heißt Pause für Node und Miner, CoreStarter hält den Mac nicht wach.
- Kein Intel-Mac, kein macOS vor 15.
