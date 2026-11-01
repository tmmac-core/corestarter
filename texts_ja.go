package main

import "fmt"

var textsJA = texts{
	lang:         "ja",
	thousandsSep: ",",
	appName:      "CoreStarter",
	starting:     "ノードを起動しています…",
	stopping:     "ノードを終了しています…",
	stopped:      "ノード停止",
	failed:       "ノードが繰り返しクラッシュしました – 30分後に再試行、ログを参照",
	diskFull:     "ディスク容量不足 – ノードを一時停止",
	searching:    "他のノードを探しています…",
	waiting:      "同期の開始を待っています…",
	peers: func(n uint64) string {
		if n == 0 {
			return ""
		}
		return fmt.Sprintf("%d個のノードに接続中", n)
	},
	syncBlocks: func(pct int, cur, high string) string {
		return fmt.Sprintf("同期中：%d%%（ブロック %s / %s）", pct, cur, high)
	},
	syncState: func(pulled string) string {
		return fmt.Sprintf("アカウント状態を読み込み中（%s件）– 最後の段階で、数時間かかることがあります", pulled)
	},
	catchingUp: func(pct int, behind string) string {
		return fmt.Sprintf("追いついています：%d%%（残り%sブロック）", pct, behind)
	},
	checking: func(head string) string {
		return fmt.Sprintf("新しいブロックを確認中 · ブロック %s", head)
	},
	noPeers: func(head string) string {
		return fmt.Sprintf("他のノードに接続できません · ブロック %s", head)
	},
	synced: func(head string) string { return fmt.Sprintf("最新の状態です · ブロック %s", head) },

	menuOpenData:  "データフォルダーを開く",
	menuOpenLog:   "ログフォルダーを開く",
	menuSettings:  "設定…",
	menuMiner:     "マイナーを表示…",
	tipMining:     func(rate string) string { return "マイニング " + rate + " H/s" },
	menuRestart:   "ノードを再起動",
	menuUninstall: "アンインストール…",
	menuQuit:      "終了",

	nodeOff:         "ノードはオフです",
	setupNeeded:     "セットアップが未完了です – 「設定…」を開いてください",
	minerStarting:   "マイニング：起動中（セルフテスト中）…",
	minerConnecting: "マイニング：プールに接続中…",
	minerPaused:     "マイニング一時停止中（バッテリー駆動）",
	minerConsent:    "マイニングは同意待ちです（設定…）",
	minerHot:        "マイニング一時停止中（高温）",
	minerFailed: func(code string) string {
		r := map[string]string{
			"selftest":  "セルフテストで計算結果の誤りが見つかりました",
			"login":     "プールがアドレスを拒否しました",
			"address":   "無効なアドレス",
			"job":       "マイナーをCoreStarterに関連付けられませんでした",
			"av":        "core-miner.exeがウイルス対策ソフトにより削除またはブロックされました – CoreStarterのフォルダーを除外に追加してから、もう一度保存してください",
			"crashloop": "繰り返しクラッシュしました。30分後に再試行します",
		}[code]
		if r == "" {
			r = "エラー。詳しくはminer.logを参照"
		}
		return "マイニング停止：" + r
	},
	minerRunning: func(rate string, accepted int64, throttled bool) string {
		s := fmt.Sprintf("マイニング：%s H/s · シェア%d件承認", rate, accepted)
		if throttled {
			s += " · ノードの同期中は性能を抑えています"
		}
		return s
	},
	minerOffline: func(min int) string {
		return fmt.Sprintf("マイニング：%d分間プールに接続できません（ウイルス対策ソフト？ファイアウォール？インターネット？）。再試行を続けています…", min)
	},
	noAdmin: "CoreStarterは管理者権限なしで、ご自身のユーザーアカウント用に動作します。通常どおり起動してください（「管理者として実行」は使わないでください）。",
	windowFailed: func(diag string) string {
		return "このPCではCoreStarterのウィンドウを表示できませんでした。技術的な詳細は次の場所にあります：" + diag
	},
	noWebView: "設定ウィンドウを表示するには、CoreStarterに「Microsoft Edge WebView2 Runtime」が必要です（Windows 10と11には通常インストール済みです）。Microsoftのダウンロードページを今すぐ開きますか？",

	installBody: "このプログラムはCoreブロックチェーンのノード（gocore、公式バージョン %s）をインストールして起動します。\n\n" +
		"• CoreStarterはTell Me More About Coreによるベータ版ソフトウェアです。ライセンスと責任に関する条件（インストール後、プログラムフォルダー内のファイルNOTICE-core-starter.txt）と、tellmemoreaboutcore.com/privacy/#apps のプライバシーに関する説明が適用されます。\n" +
		"• ディスク：約%d GBの空き容量が必要です。チェーンは今後も増え続けます。\n" +
		"• メモリ：ノードの動作中は約2–3 GB（推定）。\n" +
		"• インターネット：初回同期で数GBをダウンロードし、その後もバックグラウンドで常に少し通信します。\n" +
		"• ネットワーク：他のノードが接続できるよう、ノードはUPnPでルーターのポートを自動で開放します。他のノード、タイムサーバー（pool.ntp.org）、DNSサーバーにはIPアドレスが見えます。\n" +
		"• CoreStarter自体はTMMACや第三者にデータを送信しません。\n" +
		"• Windowsファイアウォール：確認が表示されたら「許可」を選んでください（管理者パスワードが必要な場合があります）。許可しなくてもノードは動作しますが、ネットワークへの貢献は小さくなります。\n" +
		"• CoreStarterはWindowsと同時に自動では起動しません。\n\n" +
		"「はい」を選ぶと、これらの注意事項を読み、同意したことになります。インストールして開始しますか？",
	alreadyRunning: "CoreStarterはすでに動作しています。タスクバー右下にアイコンがあります（^ の矢印の下に隠れている場合があります）。",
	updatePrompt:   "CoreStarterは現在動作中です。このバージョンを今すぐインストールしますか？\n\nそのためにノードを正常に終了し、その後再起動します。ブロックチェーンのデータはそのまま残ります。",
	newerInstalled: func(installed, this string) string {
		return fmt.Sprintf("より新しいバージョン（%s）がすでにインストールされています。このファイル（バージョン %s）はインストールされません。", installed, this)
	},
	notEnoughSpace: func(freeGB, needGB int) string {
		return fmt.Sprintf("空き容量が足りません：空き%d GB、最低%d GB必要です。", freeGB, needGB)
	},
	unsupportedPath: "ユーザーフォルダーのパスに「$」が含まれています。この場合、ノードはデータフォルダーを確実に見つけられないため、インストールを中止します。",
	installFailed:   func(err error) string { return "インストールに失敗しました：\n\n" + err.Error() },
	uninstallFailed: func(err error) string {
		return "アンインストールに失敗しました：\n\n" + err.Error() + "\n\n「アプリと機能」からもう一度実行できます。"
	},
	installedHint: "CoreStarterが動作を開始しました。タスクバー右下のアイコンに進行状況が表示されます（^ の矢印の下に隠れている場合は、そこからタスクバーへドラッグできます）。\n\n" +
		"初回同期には、PCとインターネット環境によって数時間かかります。",
	uninstallConfirm: "CoreStarterをアンインストールしますか？ノードは終了します。",
	uninstallKeepData: func(sizeGB string) string {
		return fmt.Sprintf("ブロックチェーンのデータ（%s GB）も削除しますか？\n\n「いいえ」を選ぶとデータは残り、後で再インストールしても再同期は不要です。", sizeGB)
	},
	uninstallDone: "CoreStarterをアンインストールしました。",
	uninstallDoneKeptData: func(dir string) string {
		return "CoreStarterをアンインストールしました。ブロックチェーンのデータは次の場所に残っています：\n\n" + dir
	},
	keystoreKept: func(dir string) string {
		return "データフォルダーにアカウントの鍵（keystore）があります。安全のため削除していません：\n\n" + dir
	},
	updateTimeout: "実行中のバージョンが時間内に終了しませんでした。タスクバーのアイコンからCoreStarterを終了し（右クリック → 終了）、もう一度お試しください。",
	crashHint:     "ノードが繰り返しクラッシュしました。CoreStarterは30分ごとに再試行します。原因はログで確認できます（アイコンを右クリック → ログフォルダーを開く）。",
	diskFullHint:  "ドライブの空き容量がほとんどありません。システムが通常どおり動作するよう、CoreStarterはノードを一時停止しました。2 GB以上の空き容量を確保してください。CoreStarterは30分ごとに再試行します。",
}
