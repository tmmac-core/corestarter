package main

import "fmt"

// roCount returns n with the Romanian plural form of a counted noun:
// 1 → one, 0 and 2–19 (also 101–119, …) → few, 20+ → other ("20 de noduri").
func roCount(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	if r := n % 100; r >= 20 || (n >= 100 && r == 0) {
		return fmt.Sprintf("%d de %s", n, many)
	}
	return fmt.Sprintf("%d %s", n, many)
}

var textsRO = texts{
	lang:         "ro",
	thousandsSep: ".",
	appName:      "CoreStarter",
	starting:     "Nodul pornește …",
	stopping:     "Nodul se oprește …",
	stopped:      "Nod oprit",
	failed:       "Nodul s-a blocat de mai multe ori – nouă încercare în 30 min, vezi jurnalul",
	diskFull:     "Spațiu insuficient – nod oprit",
	searching:    "Caut alte noduri …",
	waiting:      "Aștept pornirea sincronizării …",
	peers: func(n uint64) string {
		switch n {
		case 0:
			return ""
		case 1:
			return "Conectat la 1 nod"
		}
		return "Conectat la " + roCount(int64(n), "nod", "noduri")
	},
	syncBlocks: func(pct int, cur, high string) string {
		return fmt.Sprintf("Sincronizare: %d %% (blocul %s din %s)", pct, cur, high)
	},
	syncState: func(pulled string) string {
		return fmt.Sprintf("Se încarcă soldurile conturilor (intrări: %s) – ultimul pas, poate dura ore", pulled)
	},
	catchingUp: func(pct int, behind string) string {
		return fmt.Sprintf("Recuperează: %d %% (blocuri rămase: %s)", pct, behind)
	},
	checking: func(head string) string { return fmt.Sprintf("Verifică blocuri noi · blocul %s", head) },
	noPeers:  func(head string) string { return fmt.Sprintf("Nicio conexiune la alte noduri · blocul %s", head) },
	synced:   func(head string) string { return fmt.Sprintf("La zi · blocul %s", head) },

	menuOpenData:  "Deschide folderul de date",
	menuOpenLog:   "Deschide folderul de jurnale",
	menuSettings:  "Setări …",
	menuMiner:     "Arată minerul …",
	tipMining:     func(rate string) string { return "Minare " + rate + " H/s" },
	menuRestart:   "Repornește nodul",
	menuUninstall: "Dezinstalează …",
	menuQuit:      "Ieșire",

	nodeOff:         "Nod dezactivat",
	setupNeeded:     "Configurare neterminată – deschide „Setări …”",
	minerStarting:   "Minare: pornește (autotest în curs) …",
	minerConnecting: "Minare: mă conectez la pool …",
	minerPaused:     "Minare în pauză (pe baterie)",
	minerConsent:    "Minarea așteaptă acordul tău (Setări …)",
	minerHot:        "Minare în pauză (prea fierbinte)",
	minerFailed: func(code string) string {
		r := map[string]string{
			"selftest":  "autotestul a dat rezultate de calcul greșite",
			"login":     "pool-ul a respins adresa",
			"address":   "adresă invalidă",
			"job":       "minerul nu a putut fi legat de CoreStarter",
			"av":        "core-miner.exe a fost eliminat sau blocat de antivirus – adaugă o excepție pentru folderul CoreStarter, apoi salvează din nou",
			"crashloop": "s-a blocat de mai multe ori, nouă încercare în 30 min",
		}[code]
		if r == "" {
			r = "eroare, detalii în miner.log"
		}
		return "Minare oprită: " + r
	},
	minerRunning: func(rate string, accepted int64, throttled bool) string {
		var sh string
		if accepted == 1 {
			sh = "1 share acceptat"
		} else {
			sh = roCount(accepted, "share", "share-uri") + " acceptate"
		}
		s := fmt.Sprintf("Minare: %s H/s · %s", rate, sh)
		if throttled {
			s += " · limitat cât timp nodul se sincronizează"
		}
		return s
	},
	minerOffline: func(min int) string {
		return fmt.Sprintf("Minare: pool-ul nu este accesibil de %d min (antivirus, firewall, internet?), încerc în continuare …", min)
	},
	noAdmin: "CoreStarter rulează fără drepturi de administrator, pentru contul tău de utilizator. Te rugăm să-l pornești normal (nu cu „Executare ca administrator”).",
	windowFailed: func(diag string) string {
		return "Fereastra CoreStarter nu a putut fi afișată pe acest PC. Detaliile tehnice se află în " + diag + "."
	},
	noWebView: "Pentru fereastra de setări, CoreStarter are nevoie de „Microsoft Edge WebView2 Runtime” (pe Windows 10 și 11 de obicei deja prezent). Deschizi acum pagina de descărcare Microsoft?",

	installBody: "Acest program instalează un nod al blockchain-ului Core (gocore, versiunea oficială %s) și îl pornește.\n\n" +
		"• CoreStarter este software beta de la Tell Me More About Core. Se aplică condițiile de licență și de răspundere (după instalare, în fișierul NOTICE-core-starter.txt din folderul programului), precum și informarea privind confidențialitatea de pe tellmemoreaboutcore.com/privacy/#apps.\n" +
		"• Spațiu de stocare: e nevoie de aproximativ %d GB liberi, iar lanțul continuă să crească.\n" +
		"• Memorie RAM: aproximativ 2–3 GB cât timp rulează nodul (estimare).\n" +
		"• Internet: prima sincronizare descarcă mai mulți GB, apoi rulează constant puțin trafic în fundal.\n" +
		"• Rețea: nodul deschide automat prin UPnP un port pe routerul tău, ca alte noduri să-l poată accesa. Alte noduri, serverele de timp (pool.ntp.org) și serverele DNS îți văd adresa IP.\n" +
		"• Starterul însuși nu trimite date către TMMAC sau terți.\n" +
		"• Firewall Windows: dacă Windows întreabă, alege \"Permite\" (poate cere o parolă de administrator). Fără permisiune, nodul rulează totuși, dar ajută mai puțin rețeaua.\n" +
		"• CoreStarter nu pornește automat odată cu Windows.\n\n" +
		"Alegând „Da”, confirmi că ai citit aceste informații și ești de acord. Instalezi și pornești?",
	alreadyRunning: "CoreStarter rulează deja. Îl găsești ca pictogramă în dreapta jos, în bara de activități (eventual sub săgeata ^).",
	updatePrompt:   "CoreStarter rulează acum. Instalezi această versiune acum?\n\nPentru asta, nodul va fi oprit corect și apoi repornit. Datele blockchain se păstrează.",
	newerInstalled: func(installed, this string) string {
		return fmt.Sprintf("Este deja instalată o versiune mai nouă (%s). Acest fișier (versiunea %s) nu va fi instalat.", installed, this)
	},
	notEnoughSpace: func(freeGB, needGB int) string {
		return fmt.Sprintf("Prea puțin spațiu liber: %d GB liberi, sunt necesari cel puțin %d GB.", freeGB, needGB)
	},
	unsupportedPath: "Calea folderului tău de utilizator conține caracterul \"$\". Din cauza asta, nodul nu își poate găsi în mod sigur folderul de date, așa că instalarea se anulează.",
	installFailed:   func(err error) string { return "Instalarea a eșuat:\n\n" + err.Error() },
	uninstallFailed: func(err error) string {
		return "Dezinstalarea a eșuat:\n\n" + err.Error() + "\n\nO poți porni din nou din \"Aplicații și caracteristici\"."
	},
	installedHint: "CoreStarter rulează acum. Pictograma din dreapta jos, din bara de activități, arată progresul (eventual ascunsă sub săgeata ^ – de acolo o poți trage și în bara de activități).\n\n" +
		"Prima sincronizare durează câteva ore, în funcție de PC și de internet.",
	uninstallConfirm: "Dezinstalezi CoreStarter? Nodul va fi oprit.",
	uninstallKeepData: func(sizeGB string) string {
		return fmt.Sprintf("Ștergi și datele blockchain (%s GB)?\n\n\"Nu\" le păstrează, iar o reinstalare ulterioară nu va mai trebui să se sincronizeze din nou.", sizeGB)
	},
	uninstallDone: "CoreStarter a fost dezinstalat.",
	uninstallDoneKeptData: func(dir string) string {
		return "CoreStarter a fost dezinstalat. Datele blockchain se află în continuare în:\n\n" + dir
	},
	keystoreKept: func(dir string) string {
		return "În folderul de date se află chei de cont (keystore). Din motive de siguranță, nu au fost șterse:\n\n" + dir
	},
	updateTimeout: "Versiunea care rulează nu s-a închis la timp. Te rugăm să închizi CoreStarter din pictograma din bara de activități (clic dreapta → Ieșire) și să încerci din nou.",
	crashHint:     "Nodul s-a blocat de mai multe ori. Starterul reîncearcă la fiecare 30 de minute. Motivul se află în jurnal (clic dreapta pe pictogramă → Deschide folderul de jurnale).",
	diskFullHint:  "Pe unitate aproape că nu mai este spațiu liber. Ca sistemul să funcționeze normal în continuare, CoreStarter a oprit nodul. Eliberează cel puțin 2 GB; starterul reîncearcă la fiecare 30 de minute.",
}
