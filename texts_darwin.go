//go:build darwin

package main

// macOS wording for the dialog texts that name Windows things, plus what only
// the Mac has (menus, the Applications folder). Everything else is shared.
// JA/NL/RO: unchecked by native speakers (docs/i18n-review.md).
func init() {
	for lang, x := range macTexts {
		t, ok := textsByLang[lang]
		if !ok {
			continue
		}
		t.noAdmin, t.crashHint, t.minerHot = x.noRoot, x.crashHint, x.minerHot
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
	}
}

type macText struct {
	noRoot, crashHint, minerHot           string
	minerPrefix, minerMissing, minerGuard string
	moveToApps, uninstallConfirm          string
	// menu: application, close window (Cmd+Q), Edit, Undo, Redo, Cut, Copy,
	// Paste, Select All, Window, Minimize, Close, Yes, No (webview_darwin.m)
	menu [14]string
}

// macMenu: the window app's menu and dialog button labels.
func (t texts) macMenu() []string {
	x, ok := macTexts[t.lang]
	if !ok {
		x = macTexts["en"]
	}
	return x.menu[:]
}

func (t texts) mac() macText {
	if x, ok := macTexts[t.lang]; ok {
		return x
	}
	return macTexts["en"]
}

var macTexts = map[string]macText{
	"de": {
		minerHot:         "Minen pausiert (Mac zu heiß)",
		noRoot:           "CoreStarter läuft ohne Administratorrechte, für dein eigenes Benutzerkonto. Bitte normal öffnen (nicht mit sudo).",
		crashHint:        "Die Node ist mehrfach abgestürzt. Der Starter versucht es alle 30 Minuten erneut. Im Log steht der Grund (Symbol in der Menüleiste → Log-Ordner öffnen).",
		minerPrefix:      "Minen gestoppt: ",
		minerMissing:     "das Miner-Programm fehlt – CoreStarter ist beschädigt oder nur teilweise ersetzt. Bitte CoreStarter neu aus der DMG in den Ordner Programme ziehen.",
		minerGuard:       "die Miner-Überwachung konnte nicht starten. Bitte CoreStarter beenden und neu öffnen.",
		moveToApps:       "Bitte ziehe CoreStarter zuerst in den Ordner Programme und öffne es dort. Von hier aus (Download oder DMG) richtet CoreStarter nichts ein. Die beiden Ordner öffnen sich gleich.",
		uninstallConfirm: "CoreStarter deinstallieren? Node und Miner stoppen, der Hintergrunddienst wird entfernt und die App kommt in den Papierkorb. Die Blockchain-Daten bleiben im Ordner „Library/Application Support/Core Starter“ in deinem Benutzerordner.",
		menu:             [14]string{"CoreStarter", "Fenster schließen", "Bearbeiten", "Widerrufen", "Wiederholen", "Ausschneiden", "Kopieren", "Einsetzen", "Alles auswählen", "Fenster", "Im Dock ablegen", "Schließen", "Ja", "Nein"},
	},
	"en": {
		minerHot:         "Mining paused (Mac too hot)",
		noRoot:           "CoreStarter runs without administrator rights, for your own user account. Please open it normally (not with sudo).",
		crashHint:        "The node crashed repeatedly. The starter retries every 30 minutes. The log shows why (menu bar icon → Open log folder).",
		minerPrefix:      "Mining stopped: ",
		minerMissing:     "the miner program is missing – CoreStarter is damaged or only partly replaced. Please drag CoreStarter from the DMG into the Applications folder again.",
		minerGuard:       "the miner guard could not start. Please quit CoreStarter and open it again.",
		moveToApps:       "Please drag CoreStarter into the Applications folder first and open it there. From here (a download or the DMG) CoreStarter sets nothing up. Both folders open in a moment.",
		uninstallConfirm: "Uninstall CoreStarter? Node and miner stop, the background service is removed and the app goes to the Trash. The blockchain data stays in the folder “Library/Application Support/Core Starter” in your home folder.",
		menu:             [14]string{"CoreStarter", "Close Window", "Edit", "Undo", "Redo", "Cut", "Copy", "Paste", "Select All", "Window", "Minimize", "Close", "Yes", "No"},
	},
	"ja": {
		minerHot:         "マイニング一時停止中（Macが高温）",
		noRoot:           "CoreStarterは管理者権限なしで、ご自身のユーザーアカウント用に動作します。通常どおり開いてください（sudoは使わないでください）。",
		crashHint:        "ノードが繰り返しクラッシュしました。CoreStarterは30分ごとに再試行します。原因はログで確認できます（メニューバーのアイコン → ログフォルダーを開く）。",
		minerPrefix:      "マイニング停止：",
		minerMissing:     "マイナーのプログラムがありません。CoreStarterが壊れているか、置き換えが途中で止まっています。DMGからCoreStarterをもう一度「アプリケーション」フォルダーにドラッグしてください。",
		minerGuard:       "マイナーの監視を開始できませんでした。CoreStarterを終了して、もう一度開いてください。",
		moveToApps:       "まずCoreStarterを「アプリケーション」フォルダーにドラッグし、そこから開いてください。ここ（ダウンロードやDMG）からは何も設定しません。まもなく2つのフォルダーが開きます。",
		uninstallConfirm: "CoreStarterをアンインストールしますか？ノードとマイナーが停止し、バックグラウンドサービスが削除され、アプリはゴミ箱に移動します。ブロックチェーンのデータは、ホームフォルダー内の「Library/Application Support/Core Starter」に残ります。",
		menu:             [14]string{"CoreStarter", "ウィンドウを閉じる", "編集", "取り消す", "やり直す", "カット", "コピー", "ペースト", "すべてを選択", "ウインドウ", "しまう", "閉じる", "はい", "いいえ"},
	},
	"nl": {
		minerHot:         "Minen gepauzeerd (Mac te warm)",
		noRoot:           "CoreStarter draait zonder beheerdersrechten, voor je eigen gebruikersaccount. Open het programma normaal (niet met sudo).",
		crashHint:        "De node is meerdere keren gecrasht. De starter probeert het elke 30 minuten opnieuw. In de log staat de reden (pictogram in de menubalk → Logmap openen).",
		minerPrefix:      "Minen gestopt: ",
		minerMissing:     "het minerprogramma ontbreekt – CoreStarter is beschadigd of maar half vervangen. Sleep CoreStarter opnieuw vanuit de DMG naar de map Apps.",
		minerGuard:       "de minerbewaking kon niet starten. Sluit CoreStarter af en open het opnieuw.",
		moveToApps:       "Sleep CoreStarter eerst naar de map Apps en open het daar. Vanaf hier (een download of de DMG) stelt CoreStarter niets in. Beide mappen gaan zo open.",
		uninstallConfirm: "CoreStarter verwijderen? Node en miner stoppen, de achtergronddienst wordt verwijderd en de app gaat naar de prullenmand. De blockchaingegevens blijven in de map “Library/Application Support/Core Starter” in je gebruikersmap.",
		menu:             [14]string{"CoreStarter", "Venster sluiten", "Wijzig", "Herstel", "Opnieuw", "Knip", "Kopieer", "Plak", "Selecteer alles", "Venster", "Minimaliseer", "Sluit", "Ja", "Nee"},
	},
	"ro": {
		minerHot:         "Minare în pauză (Mac prea fierbinte)",
		noRoot:           "CoreStarter rulează fără drepturi de administrator, pentru contul tău de utilizator. Te rugăm să-l deschizi normal (nu cu sudo).",
		crashHint:        "Nodul s-a blocat de mai multe ori. Starterul reîncearcă la fiecare 30 de minute. Motivul se află în jurnal (pictograma din bara de meniu → Deschide folderul de jurnale).",
		minerPrefix:      "Minare oprită: ",
		minerMissing:     "programul minerului lipsește – CoreStarter este deteriorat sau înlocuit doar parțial. Te rugăm să tragi din nou CoreStarter din DMG în dosarul Aplicații.",
		minerGuard:       "supravegherea minerului nu a putut porni. Închide CoreStarter și deschide-l din nou.",
		moveToApps:       "Te rugăm să tragi mai întâi CoreStarter în dosarul Aplicații și să-l deschizi de acolo. De aici (o descărcare sau DMG-ul) CoreStarter nu configurează nimic. Ambele dosare se deschid imediat.",
		uninstallConfirm: "Dezinstalezi CoreStarter? Nodul și minerul se opresc, serviciul de fundal este eliminat, iar aplicația ajunge în Coș. Datele blockchain rămân în dosarul „Library/Application Support/Core Starter” din dosarul tău de utilizator.",
		menu:             [14]string{"CoreStarter", "Închide fereastra", "Editare", "Anulează", "Refă", "Taie", "Copiază", "Lipește", "Selectează tot", "Fereastră", "Minimizează", "Închide", "Da", "Nu"},
	},
}
