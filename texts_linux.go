//go:build linux

package main

import (
	"fmt"
	"os"
)

// Linux wording for the few dialog texts that name Windows things; everything
// else is shared. JA/NL/RO: unchecked by native speakers (docs/i18n-review.md).
func init() {
	for lang, x := range map[string]struct{ noRoot, crashHint string }{
		"de": {
			"CoreStarter läuft ohne Root-Rechte, für dein eigenes Benutzerkonto. Bitte normal starten (nicht mit sudo).",
			"Die Node ist mehrfach abgestürzt. Der Starter versucht es alle 30 Minuten erneut. Im Log steht der Grund (CoreStarter öffnen → Logs).",
		},
		"en": {
			"CoreStarter runs without root rights, for your own user account. Please start it normally (not with sudo).",
			"The node crashed repeatedly. The starter retries every 30 minutes. The log shows why (open CoreStarter → Logs).",
		},
		"ja": {
			"CoreStarterはroot権限なしで、ご自身のユーザーアカウント用に動作します。通常どおり起動してください（sudoは使わないでください）。",
			"ノードが繰り返しクラッシュしました。CoreStarterは30分ごとに再試行します。原因はログで確認できます（CoreStarterを開く → ログ）。",
		},
		"nl": {
			"CoreStarter draait zonder rootrechten, voor je eigen gebruikersaccount. Start het programma normaal (niet met sudo).",
			"De node is meerdere keren gecrasht. De starter probeert het elke 30 minuten opnieuw. In de log staat de reden (CoreStarter openen → Logs).",
		},
		"ro": {
			"CoreStarter rulează fără drepturi root, pentru contul tău de utilizator. Te rugăm să-l pornești normal (nu cu sudo).",
			"Nodul s-a blocat de mai multe ori. Starterul reîncearcă la fiecare 30 de minute. Motivul se află în jurnal (deschide CoreStarter → Jurnale).",
		},
	} {
		if t, ok := textsByLang[lang]; ok {
			t.noAdmin, t.crashHint = x.noRoot, x.crashHint
		}
	}
	for lang, x := range linuxFailures {
		t, ok := textsByLang[lang]
		if !ok {
			continue
		}
		base, x := t.minerFailed, x
		t.minerFailed = func(code string) string {
			switch code {
			case "av":
				return x.minerPrefix + x.minerMissing
			case "job":
				return x.minerPrefix + x.minerGuard
			}
			return base(code)
		}
		t.windowFailed = func(logFile string) string {
			if _, err := os.Stat(logFile); err != nil {
				return x.window
			}
			return x.window + " " + fmt.Sprintf(x.windowLog, logFile)
		}
	}
}

// linuxFailures: the miner's "removed by the antivirus" and "job object" codes
// mean something else here (package removed or half updated; the parent-death
// guard), and a window that cannot be shown needs a hint for drivers and VMs.
var linuxFailures = map[string]struct{ minerPrefix, minerMissing, minerGuard, window, windowLog string }{
	"de": {"Minen gestoppt: ",
		"das Miner-Programm fehlt – CoreStarter wurde entfernt oder nur teilweise aktualisiert. Bitte das Paket neu installieren.",
		"die Miner-Überwachung konnte nicht starten. Bitte CoreStarter beenden und neu öffnen.",
		"Das Fenster von CoreStarter lässt sich auf diesem Rechner nicht anzeigen. Meist liegt das am Grafiktreiber oder an einer virtuellen Maschine ohne 3D-Beschleunigung: Grafiktreiber aktualisieren bzw. 3D in der VM einschalten, dann erneut öffnen.",
		"Technische Details: %s"},
	"en": {"Mining stopped: ",
		"the miner program is missing – CoreStarter was removed or only partly updated. Please install the package again.",
		"the miner guard could not start. Please quit CoreStarter and open it again.",
		"CoreStarter's window cannot be shown on this computer. This is usually the graphics driver or a virtual machine without 3D acceleration: update the graphics driver or enable 3D in the VM, then open it again.",
		"Technical details: %s"},
	"ja": {"マイニング停止：",
		"マイナーのプログラムがありません。CoreStarterが削除されたか、更新が途中で止まっています。パッケージをもう一度インストールしてください。",
		"マイナーの監視を開始できませんでした。CoreStarterを終了して、もう一度開いてください。",
		"このコンピューターではCoreStarterのウィンドウを表示できません。多くの場合、グラフィックドライバーか、3Dアクセラレーションのない仮想マシンが原因です。グラフィックドライバーを更新するか、仮想マシンで3Dを有効にしてから、もう一度開いてください。",
		"技術的な詳細: %s"},
	"nl": {"Minen gestopt: ",
		"het minerprogramma ontbreekt – CoreStarter is verwijderd of maar half bijgewerkt. Installeer het pakket opnieuw.",
		"de minerbewaking kon niet starten. Sluit CoreStarter af en open het opnieuw.",
		"Het venster van CoreStarter kan op deze computer niet worden getoond. Meestal ligt dat aan het grafische stuurprogramma of aan een virtuele machine zonder 3D-versnelling: werk het stuurprogramma bij of zet 3D aan in de VM en open het daarna opnieuw.",
		"Technische details: %s"},
	"ro": {"Minare oprită: ",
		"programul minerului lipsește – CoreStarter a fost eliminat sau actualizat doar parțial. Te rugăm să reinstalezi pachetul.",
		"supravegherea minerului nu a putut porni. Închide CoreStarter și deschide-l din nou.",
		"Fereastra CoreStarter nu poate fi afișată pe acest calculator. De obicei cauza este driverul grafic sau o mașină virtuală fără accelerare 3D: actualizează driverul grafic sau activează 3D în mașina virtuală, apoi deschide din nou.",
		"Detalii tehnice: %s"},
}

// noTrayHint: notification on desktops without tray icons (plain GNOME).
var noTrayHint = map[string]string{
	"de": "CoreStarter läuft im Hintergrund. Stand, Einstellungen und Beenden: „CoreStarter“ im App-Menü.",
	"en": "CoreStarter is running in the background. Progress, settings and quit: “CoreStarter” in the app menu.",
	"ja": "CoreStarterはバックグラウンドで動作しています。状況・設定・終了は、アプリメニューの「CoreStarter」から行えます。",
	"nl": "CoreStarter draait op de achtergrond. Stand, instellingen en afsluiten: “CoreStarter” in het app-menu.",
	"ro": "CoreStarter rulează în fundal. Stare, setări și ieșire: „CoreStarter” în meniul de aplicații.",
}
