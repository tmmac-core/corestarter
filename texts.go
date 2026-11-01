package main

import "fmt"

// texts holds every user-facing string, one set per language (uiLangs). The
// language comes from settings.json ("lang"), else from Windows (systemTexts).
type texts struct {
	lang         string // "de", "en", ... as in uiLangs and the window's L
	thousandsSep string

	appName string

	starting, stopping, stopped, failed, diskFull, searching, waiting string
	peers                                                             func(n uint64) string
	syncBlocks                                                        func(pct int, cur, high string) string
	syncState                                                         func(pulled string) string
	catchingUp                                                        func(pct int, behind string) string
	checking, noPeers, synced                                         func(head string) string

	menuOpenData, menuOpenLog, menuSettings, menuRestart, menuUninstall, menuQuit string
	menuMiner                                                                     string
	tipMining                                                                     func(rate string) string

	nodeOff, setupNeeded                        string
	minerStarting, minerConnecting, minerPaused string
	minerConsent                                string                   // mining is on, but the notes for it are not accepted yet
	minerHot                                    string                   // macOS names the Mac (texts_darwin.go)
	minerFailed                                 func(code string) string // code: see minerSup.loop
	minerRunning                                func(rate string, accepted int64, throttled bool) string
	noWebView                                   string
	windowFailed                                func(diag string) string
	noAdmin                                     string
	minerOffline                                func(minutes int) string

	installBody                    string
	alreadyRunning                 string
	updatePrompt                   string
	newerInstalled                 func(installed, this string) string
	notEnoughSpace                 func(freeGB, needGB int) string
	unsupportedPath                string
	installFailed, uninstallFailed func(err error) string
	installedHint                  string
	uninstallConfirm               string
	uninstallKeepData              func(sizeGB string) string
	uninstallDone                  string
	uninstallDoneKeptData          func(dir string) string
	keystoreKept                   func(dir string) string
	updateTimeout                  string
	crashHint, diskFullHint        string
}

var textsDE = texts{
	lang:         "de",
	thousandsSep: ".",
	appName:      "CoreStarter",
	starting:     "Node startet …",
	stopping:     "Node wird beendet …",
	stopped:      "Node gestoppt",
	failed:       "Node mehrfach abgestürzt – neuer Versuch in 30 min, siehe Log",
	diskFull:     "Speicherplatz knapp – Node angehalten",
	searching:    "Suche andere Nodes …",
	waiting:      "Warte auf Start der Synchronisierung …",
	peers: func(n uint64) string {
		switch n {
		case 0:
			return ""
		case 1:
			return "Verbunden mit 1 Node"
		}
		return fmt.Sprintf("Verbunden mit %d Nodes", n)
	},
	syncBlocks: func(pct int, cur, high string) string {
		return fmt.Sprintf("Synchronisierung: %d %% (Block %s von %s)", pct, cur, high)
	},
	syncState: func(pulled string) string {
		return fmt.Sprintf("Lädt Kontostände (%s Einträge) – letzter Schritt, kann Stunden dauern", pulled)
	},
	catchingUp: func(pct int, behind string) string {
		return fmt.Sprintf("Holt auf: %d %% (noch %s Blöcke)", pct, behind)
	},
	checking: func(head string) string { return fmt.Sprintf("Prüft auf neue Blöcke · Block %s", head) },
	noPeers:  func(head string) string { return fmt.Sprintf("Keine Verbindung zu anderen Nodes · Block %s", head) },
	synced:   func(head string) string { return fmt.Sprintf("Auf dem neuesten Stand · Block %s", head) },

	menuOpenData:  "Daten-Ordner öffnen",
	menuOpenLog:   "Log-Ordner öffnen",
	menuSettings:  "Einstellungen …",
	menuMiner:     "Miner anzeigen …",
	tipMining:     func(rate string) string { return "Minen " + rate + " H/s" },
	menuRestart:   "Node neu starten",
	menuUninstall: "Deinstallieren …",
	menuQuit:      "Beenden",

	nodeOff:         "Node ausgeschaltet",
	setupNeeded:     "Einrichtung offen – „Einstellungen …“ öffnen",
	minerStarting:   "Minen: startet (Selbsttest läuft) …",
	minerConnecting: "Minen: verbinde mit dem Pool …",
	minerPaused:     "Minen pausiert (Akkubetrieb)",
	minerConsent:    "Minen wartet auf deine Zustimmung (Einstellungen …)",
	minerHot:        "Minen pausiert (zu heiß)",
	minerFailed: func(code string) string {
		r := map[string]string{
			"selftest":  "der Selbsttest hat falsche Rechenergebnisse ergeben",
			"login":     "der Pool hat die Adresse abgelehnt",
			"address":   "ungültige Adresse",
			"job":       "Miner konnte nicht an CoreStarter gebunden werden",
			"av":        "core-miner.exe wurde vom Virenschutz entfernt oder blockiert – Ausnahme für den Ordner CoreStarter anlegen, dann erneut speichern",
			"crashloop": "mehrfach abgestürzt, neuer Versuch in 30 min",
		}[code]
		if r == "" {
			r = "Fehler, Details in miner.log"
		}
		return "Minen gestoppt: " + r
	},
	minerRunning: func(rate string, accepted int64, throttled bool) string {
		s := fmt.Sprintf("Minen: %s H/s · %d Shares angenommen", rate, accepted)
		if throttled {
			s += " · gedrosselt, solange die Node synchronisiert"
		}
		return s
	},
	minerOffline: func(min int) string {
		return fmt.Sprintf("Minen: Pool seit %d min nicht erreichbar (Virenscanner, Firewall, Internet?), versuche weiter …", min)
	},
	noAdmin: "CoreStarter läuft ohne Administratorrechte, für dein eigenes Benutzerkonto. Bitte normal starten (nicht „Als Administrator ausführen“).",
	windowFailed: func(diag string) string {
		return "Das Fenster von CoreStarter konnte auf diesem PC nicht angezeigt werden. Technische Details stehen in " + diag + "."
	},
	noWebView: "Für das Einstellungsfenster braucht CoreStarter die „Microsoft Edge WebView2 Runtime“ (auf Windows 10 und 11 meist schon vorhanden). Download-Seite von Microsoft jetzt öffnen?",

	installBody: "Dieses Programm installiert eine Core-Blockchain-Node (gocore, offizielle Version %s) und startet sie.\n\n" +
		"• CoreStarter ist Beta-Software von Tell Me More About Core. Es gelten die Lizenz- und Haftungsbedingungen (nach der Installation in der Datei NOTICE-core-starter.txt im Programmordner) und der Datenschutzhinweis auf tellmemoreaboutcore.com/de/datenschutz/#apps.\n" +
		"• Speicherplatz: rund %d GB frei nötig, die Blockchain wächst weiter.\n" +
		"• Arbeitsspeicher: rund 2–3 GB, solange die Node läuft (Schätzung).\n" +
		"• Internet: Die erste Synchronisierung lädt mehrere GB, danach läuft ständig etwas Datenverkehr im Hintergrund.\n" +
		"• Netzwerk: Die Node gibt per UPnP automatisch einen Port an deinem Router frei, damit andere Nodes sie erreichen. Andere Nodes, Zeitserver (pool.ntp.org) und DNS-Server sehen deine IP-Adresse.\n" +
		"• Der Starter selbst sendet keine Daten an TMMAC oder Dritte.\n" +
		"• Windows-Firewall: Falls Windows fragt, \"Zulassen\" wählen (kann ein Administrator-Passwort verlangen). Ohne Freigabe läuft die Node trotzdem, hilft dem Netz aber weniger.\n" +
		"• CoreStarter startet nicht automatisch mit Windows.\n\n" +
		"Mit „Ja“ bestätigst du, dass du die Hinweise gelesen hast und einverstanden bist. Installieren und starten?",
	alreadyRunning: "CoreStarter läuft bereits. Du findest ihn als Symbol unten rechts in der Taskleiste (ggf. unter dem Pfeil ^).",
	updatePrompt:   "CoreStarter läuft gerade. Diese Version jetzt installieren?\n\nDie Node wird dafür sauber beendet und danach neu gestartet. Die Blockchain-Daten bleiben erhalten.",
	newerInstalled: func(installed, this string) string {
		return fmt.Sprintf("Es ist bereits eine neuere Version (%s) installiert. Diese Datei (Version %s) wird nicht installiert.", installed, this)
	},
	notEnoughSpace: func(freeGB, needGB int) string {
		return fmt.Sprintf("Zu wenig freier Speicher: %d GB frei, mindestens %d GB nötig.", freeGB, needGB)
	},
	unsupportedPath: "Der Pfad deines Benutzerordners enthält ein \"$\"-Zeichen. Damit kann die Node ihren Datenordner nicht zuverlässig finden, die Installation wird abgebrochen.",
	installFailed:   func(err error) string { return "Installation fehlgeschlagen:\n\n" + err.Error() },
	uninstallFailed: func(err error) string {
		return "Deinstallation fehlgeschlagen:\n\n" + err.Error() + "\n\nDu kannst sie über \"Apps & Features\" erneut starten."
	},
	installedHint: "CoreStarter läuft jetzt. Das Symbol unten rechts in der Taskleiste zeigt den Fortschritt (ggf. unter dem Pfeil ^ versteckt – dort kannst du es auch in die Taskleiste ziehen).\n\n" +
		"Die erste Synchronisierung dauert je nach PC und Internet mehrere Stunden.",
	uninstallConfirm: "CoreStarter deinstallieren? Die Node wird beendet.",
	uninstallKeepData: func(sizeGB string) string {
		return fmt.Sprintf("Blockchain-Daten (%s GB) ebenfalls löschen?\n\n\"Nein\" behält sie, eine spätere Neuinstallation muss dann nicht neu synchronisieren.", sizeGB)
	},
	uninstallDone: "CoreStarter wurde deinstalliert.",
	uninstallDoneKeptData: func(dir string) string {
		return "CoreStarter wurde deinstalliert. Die Blockchain-Daten liegen weiter in:\n\n" + dir
	},
	keystoreKept: func(dir string) string {
		return "Im Datenordner liegen Konto-Schlüssel (keystore). Sie wurden zur Sicherheit nicht gelöscht:\n\n" + dir
	},
	updateTimeout: "Die laufende Version hat sich nicht rechtzeitig beendet. Bitte beende CoreStarter über das Symbol in der Taskleiste (Rechtsklick → Beenden) und versuche es erneut.",
	crashHint:     "Die Node ist mehrfach abgestürzt. Der Starter versucht es alle 30 Minuten erneut. Im Log steht der Grund (Rechtsklick auf das Symbol → Log-Ordner öffnen).",
	diskFullHint:  "Auf dem Laufwerk ist kaum noch Speicherplatz frei. Damit das System normal weiterarbeitet, hat CoreStarter die Node angehalten. Gib mindestens 2 GB frei; der Starter versucht es alle 30 Minuten erneut.",
}

var textsEN = texts{
	lang:         "en",
	thousandsSep: ",",
	appName:      "CoreStarter",
	starting:     "Node starting …",
	stopping:     "Node shutting down …",
	stopped:      "Node stopped",
	failed:       "Node crashed repeatedly – retrying in 30 min, see log",
	diskFull:     "Low disk space – node paused",
	searching:    "Looking for other nodes …",
	waiting:      "Waiting for sync to start …",
	peers: func(n uint64) string {
		switch n {
		case 0:
			return ""
		case 1:
			return "Connected to 1 node"
		}
		return fmt.Sprintf("Connected to %d nodes", n)
	},
	syncBlocks: func(pct int, cur, high string) string {
		return fmt.Sprintf("Syncing: %d%% (block %s of %s)", pct, cur, high)
	},
	syncState: func(pulled string) string {
		return fmt.Sprintf("Loading account state (%s entries) – final step, may take hours", pulled)
	},
	catchingUp: func(pct int, behind string) string {
		return fmt.Sprintf("Catching up: %d%% (%s blocks to go)", pct, behind)
	},
	checking: func(head string) string { return fmt.Sprintf("Checking for new blocks · block %s", head) },
	noPeers:  func(head string) string { return fmt.Sprintf("No connection to other nodes · block %s", head) },
	synced:   func(head string) string { return fmt.Sprintf("Up to date · block %s", head) },

	menuOpenData:  "Open data folder",
	menuOpenLog:   "Open log folder",
	menuSettings:  "Settings …",
	menuMiner:     "Show miner …",
	tipMining:     func(rate string) string { return "mining " + rate + " H/s" },
	menuRestart:   "Restart node",
	menuUninstall: "Uninstall …",
	menuQuit:      "Quit",

	nodeOff:         "Node switched off",
	setupNeeded:     "Setup not finished – open “Settings …”",
	minerStarting:   "Mining: starting (self-test running) …",
	minerConnecting: "Mining: connecting to the pool …",
	minerPaused:     "Mining paused (on battery)",
	minerConsent:    "Mining waits for your consent (Settings …)",
	minerHot:        "Mining paused (too hot)",
	minerFailed: func(code string) string {
		r := map[string]string{
			"selftest":  "the self-test found wrong results",
			"login":     "the pool rejected the address",
			"address":   "invalid address",
			"job":       "the miner could not be bound to CoreStarter",
			"av":        "core-miner.exe was removed or blocked by the antivirus – add an exception for the CoreStarter folder, then save again",
			"crashloop": "crashed repeatedly, next try in 30 min",
		}[code]
		if r == "" {
			r = "error, details in miner.log"
		}
		return "Mining stopped: " + r
	},
	minerRunning: func(rate string, accepted int64, throttled bool) string {
		s := fmt.Sprintf("Mining: %s H/s · %d shares accepted", rate, accepted)
		if throttled {
			s += " · throttled while the node syncs"
		}
		return s
	},
	minerOffline: func(min int) string {
		return fmt.Sprintf("Mining: pool unreachable for %d min (antivirus, firewall, internet?), still trying …", min)
	},
	noAdmin: "CoreStarter runs without administrator rights, for your own user account. Please start it normally (not “Run as administrator”).",
	windowFailed: func(diag string) string {
		return "The CoreStarter window could not be shown on this PC. Technical details are in " + diag + "."
	},
	noWebView: "The settings window needs the “Microsoft Edge WebView2 Runtime” (usually present on Windows 10 and 11). Open Microsoft's download page now?",

	installBody: "This program installs a Core Blockchain node (gocore, official version %s) and starts it.\n\n" +
		"• CoreStarter is beta software by Tell Me More About Core. The license and liability terms apply (after installing, in the file NOTICE-core-starter.txt in the program folder), as does the privacy notice at tellmemoreaboutcore.com/privacy/#apps.\n" +
		"• Disk: about %d GB free space needed, and the chain keeps growing.\n" +
		"• Memory: about 2–3 GB RAM while the node runs (estimate).\n" +
		"• Internet: the first sync downloads several GB; afterwards there is constant background traffic.\n" +
		"• Network: the node automatically opens a port on your router via UPnP so other nodes can reach it. Other nodes, time servers (pool.ntp.org) and DNS servers see your IP address.\n" +
		"• This starter itself sends no data to TMMAC or anyone else.\n" +
		"• Windows Firewall: if Windows asks, choose \"Allow\" (may require an administrator password). Without it the node still runs but helps the network less.\n" +
		"• CoreStarter does not start automatically with Windows.\n\n" +
		"Choosing \"Yes\" confirms that you have read these notes and agree. Install and start?",
	alreadyRunning: "CoreStarter is already running. Look for its icon in the bottom-right taskbar area (maybe under the ^ arrow).",
	updatePrompt:   "CoreStarter is running. Install this version now?\n\nThe node will be shut down cleanly and restarted. Your blockchain data is kept.",
	newerInstalled: func(installed, this string) string {
		return fmt.Sprintf("A newer version (%s) is already installed. This file (version %s) will not be installed.", installed, this)
	},
	notEnoughSpace: func(freeGB, needGB int) string {
		return fmt.Sprintf("Not enough free disk space: %d GB free, at least %d GB needed.", freeGB, needGB)
	},
	unsupportedPath: "Your user folder path contains a \"$\" character. The node cannot reliably find its data folder there, so installation is cancelled.",
	installFailed:   func(err error) string { return "Installation failed:\n\n" + err.Error() },
	uninstallFailed: func(err error) string {
		return "Uninstall failed:\n\n" + err.Error() + "\n\nYou can start it again from \"Apps & Features\"."
	},
	installedHint: "CoreStarter is now running. The icon in the bottom-right taskbar area shows the progress (it may be hidden under the ^ arrow – you can drag it onto the taskbar from there).\n\n" +
		"The first sync takes several hours depending on your PC and internet.",
	uninstallConfirm: "Uninstall CoreStarter? The node will be stopped.",
	uninstallKeepData: func(sizeGB string) string {
		return fmt.Sprintf("Also delete the blockchain data (%s GB)?\n\n\"No\" keeps it, so a later reinstall does not need to sync again.", sizeGB)
	},
	uninstallDone: "CoreStarter has been uninstalled.",
	uninstallDoneKeptData: func(dir string) string {
		return "CoreStarter has been uninstalled. The blockchain data is still in:\n\n" + dir
	},
	keystoreKept: func(dir string) string {
		return "The data folder contains account keys (keystore). They were kept for safety:\n\n" + dir
	},
	updateTimeout: "The running version did not quit in time. Please quit CoreStarter via its taskbar icon (right-click → Quit) and try again.",
	crashHint:     "The node crashed repeatedly. The starter retries every 30 minutes. The log shows why (right-click the icon → Open log folder).",
	diskFullHint:  "The drive is almost full. So that the system keeps working normally, CoreStarter has paused the node. Free up at least 2 GB; the starter retries every 30 minutes.",
}

// textsByLang maps the codes in uiLangs to their texts.
var textsByLang = map[string]*texts{"de": &textsDE, "en": &textsEN, "ja": &textsJA, "nl": &textsNL, "ro": &textsRO}

// textsFor returns the texts for a saved language, or Windows' language for
// "" (automatic) and unknown codes.
func textsFor(lang string) texts {
	if t, ok := textsByLang[lang]; ok {
		return *t
	}
	return systemTexts()
}
