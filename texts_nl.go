package main

import "fmt"

var textsNL = texts{
	lang:         "nl",
	thousandsSep: ".",
	appName:      "CoreStarter",
	starting:     "Node start …",
	stopping:     "Node wordt afgesloten …",
	stopped:      "Node gestopt",
	failed:       "Node meerdere keren gecrasht – nieuwe poging over 30 min, zie log",
	diskFull:     "Weinig opslag – node gestopt",
	searching:    "Zoekt andere nodes …",
	waiting:      "Wacht op start van de synchronisatie …",
	peers: func(n uint64) string {
		switch n {
		case 0:
			return ""
		case 1:
			return "Verbonden met 1 node"
		}
		return fmt.Sprintf("Verbonden met %d nodes", n)
	},
	syncBlocks: func(pct int, cur, high string) string {
		return fmt.Sprintf("Synchronisatie: %d%% (blok %s van %s)", pct, cur, high)
	},
	syncState: func(pulled string) string {
		return fmt.Sprintf("Laadt saldi (%s items) – laatste stap, kan uren duren", pulled)
	},
	catchingUp: func(pct int, behind string) string {
		return fmt.Sprintf("Haalt in: %d%% (nog %s blokken)", pct, behind)
	},
	checking: func(head string) string { return fmt.Sprintf("Controleert op nieuwe blokken · blok %s", head) },
	noPeers:  func(head string) string { return fmt.Sprintf("Geen verbinding met andere nodes · blok %s", head) },
	synced:   func(head string) string { return fmt.Sprintf("Helemaal bij · blok %s", head) },

	menuOpenData:  "Gegevensmap openen",
	menuOpenLog:   "Logmap openen",
	menuSettings:  "Instellingen …",
	menuMiner:     "Miner tonen …",
	tipMining:     func(rate string) string { return "Minen " + rate + " H/s" },
	menuRestart:   "Node opnieuw starten",
	menuUninstall: "Verwijderen …",
	menuQuit:      "Afsluiten",

	nodeOff:         "Node uitgeschakeld",
	setupNeeded:     "Installatie niet afgerond – open “Instellingen …”",
	minerStarting:   "Minen: start (zelftest loopt) …",
	minerConnecting: "Minen: verbinden met de pool …",
	minerPaused:     "Minen gepauzeerd (op accu)",
	minerConsent:    "Minen wacht op je toestemming (Instellingen …)",
	minerHot:        "Minen gepauzeerd (te warm)",
	minerFailed: func(code string) string {
		r := map[string]string{
			"selftest":  "de zelftest gaf verkeerde rekenresultaten",
			"login":     "de pool heeft het adres geweigerd",
			"address":   "ongeldig adres",
			"job":       "miner kon niet aan CoreStarter worden gekoppeld",
			"av":        "core-miner.exe is door de virusscanner verwijderd of geblokkeerd – maak een uitzondering voor de map CoreStarter en sla daarna opnieuw op",
			"crashloop": "meerdere keren gecrasht, nieuwe poging over 30 min",
		}[code]
		if r == "" {
			r = "fout, details in miner.log"
		}
		return "Minen gestopt: " + r
	},
	minerRunning: func(rate string, accepted int64, throttled bool) string {
		var s string
		if accepted == 1 {
			s = fmt.Sprintf("Minen: %s H/s · %d share geaccepteerd", rate, accepted)
		} else {
			s = fmt.Sprintf("Minen: %s H/s · %d shares geaccepteerd", rate, accepted)
		}
		if throttled {
			s += " · afgeremd zolang de node synchroniseert"
		}
		return s
	},
	minerOffline: func(min int) string {
		return fmt.Sprintf("Minen: pool al %d min niet bereikbaar (virusscanner, firewall, internet?), blijft proberen …", min)
	},
	noAdmin: "CoreStarter draait zonder beheerdersrechten, voor je eigen gebruikersaccount. Start het programma normaal (niet via “Als administrator uitvoeren”).",
	windowFailed: func(diag string) string {
		return "Het venster van CoreStarter kon op deze pc niet worden weergegeven. Technische details staan in " + diag + "."
	},
	noWebView: "Voor het instellingenvenster heeft CoreStarter de “Microsoft Edge WebView2 Runtime” nodig (op Windows 10 en 11 meestal al aanwezig). Nu de downloadpagina van Microsoft openen?",

	installBody: "Dit programma installeert een node van de Core-blockchain (gocore, officiële versie %s) en start die.\n\n" +
		"• CoreStarter is bètasoftware van Tell Me More About Core. De licentie- en aansprakelijkheidsvoorwaarden gelden (na de installatie in het bestand NOTICE-core-starter.txt in de programmamap), net als de privacyverklaring op tellmemoreaboutcore.com/privacy/#apps.\n" +
		"• Opslag: ongeveer %d GB vrij nodig, de keten blijft groeien.\n" +
		"• Werkgeheugen: ongeveer 2–3 GB zolang de node draait (schatting).\n" +
		"• Internet: de eerste synchronisatie downloadt meerdere GB, daarna is er constant wat dataverkeer op de achtergrond.\n" +
		"• Netwerk: de node opent via UPnP automatisch een poort op je router, zodat andere nodes hem kunnen bereiken. Andere nodes, tijdservers (pool.ntp.org) en DNS-servers zien je IP-adres.\n" +
		"• De starter zelf stuurt geen gegevens naar TMMAC of derden.\n" +
		"• Windows Firewall: als Windows het vraagt, kies dan \"Toestaan\" (kan om een beheerderswachtwoord vragen). Zonder toestemming draait de node toch, maar helpt hij het netwerk minder.\n" +
		"• CoreStarter start niet automatisch met Windows.\n\n" +
		"Met \"Ja\" bevestig je dat je deze informatie hebt gelezen en ermee akkoord gaat. Installeren en starten?",
	alreadyRunning: "CoreStarter draait al. Je vindt het als pictogram rechtsonder in de taakbalk (eventueel onder het pijltje ^).",
	updatePrompt:   "CoreStarter draait op dit moment. Deze versie nu installeren?\n\nDe node wordt daarvoor netjes afgesloten en daarna opnieuw gestart. De blockchaingegevens blijven behouden.",
	newerInstalled: func(installed, this string) string {
		return fmt.Sprintf("Er is al een nieuwere versie (%s) geïnstalleerd. Dit bestand (versie %s) wordt niet geïnstalleerd.", installed, this)
	},
	notEnoughSpace: func(freeGB, needGB int) string {
		return fmt.Sprintf("Te weinig vrije opslag: %d GB vrij, minstens %d GB nodig.", freeGB, needGB)
	},
	unsupportedPath: "Het pad van je gebruikersmap bevat een \"$\"-teken. Daardoor kan de node zijn gegevensmap niet betrouwbaar vinden; de installatie wordt afgebroken.",
	installFailed:   func(err error) string { return "Installatie mislukt:\n\n" + err.Error() },
	uninstallFailed: func(err error) string {
		return "Verwijderen mislukt:\n\n" + err.Error() + "\n\nJe kunt het opnieuw starten via \"Apps en onderdelen\"."
	},
	installedHint: "CoreStarter draait nu. Het pictogram rechtsonder in de taakbalk toont de voortgang (eventueel verborgen onder het pijltje ^ – daar kun je het ook naar de taakbalk slepen).\n\n" +
		"De eerste synchronisatie duurt, afhankelijk van pc en internet, meerdere uren.",
	uninstallConfirm: "CoreStarter verwijderen? De node wordt afgesloten.",
	uninstallKeepData: func(sizeGB string) string {
		return fmt.Sprintf("Blockchaingegevens (%s GB) ook verwijderen?\n\n\"Nee\" bewaart ze; een latere herinstallatie hoeft dan niet opnieuw te synchroniseren.", sizeGB)
	},
	uninstallDone: "CoreStarter is verwijderd.",
	uninstallDoneKeptData: func(dir string) string {
		return "CoreStarter is verwijderd. De blockchaingegevens staan nog in:\n\n" + dir
	},
	keystoreKept: func(dir string) string {
		return "In de gegevensmap staan accountsleutels (keystore). Die zijn voor de veiligheid niet verwijderd:\n\n" + dir
	},
	updateTimeout: "De actieve versie is niet op tijd afgesloten. Sluit CoreStarter af via het pictogram in de taakbalk (rechtsklik → Afsluiten) en probeer het opnieuw.",
	crashHint:     "De node is meerdere keren gecrasht. De starter probeert het elke 30 minuten opnieuw. In de log staat de reden (rechtsklik op het pictogram → Logmap openen).",
	diskFullHint:  "Op de schijf is bijna geen opslag meer vrij. Zodat het systeem normaal blijft werken, heeft CoreStarter de node gestopt. Maak minstens 2 GB vrij; de starter probeert het elke 30 minuten opnieuw.",
}
