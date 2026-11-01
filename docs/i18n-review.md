# Übersetzungen JA/NL/RO: Prüfliste für Muttersprachler

Die Texte stehen in `ui/index.html` (`L.ja`, `L.nl`, `L.ro`) und `texts_ja.go`, `texts_nl.go`, `texts_ro.go`. Begriffe folgen dem Core Mobile Miner. Die Übersetzungen sind maschinell erstellt; unten die Stellen, bei denen sie unsicher sind. Hinweise von Muttersprachlern sind willkommen (Issue oder Pull Request).

## CoreStarter ja – Punkte fuer Muttersprachler-Review

| Key | Text (ja) | Warum unsicher |
|---|---|---|
| L.mvSupportL, log.supStart/supDone/supSkip | 開発支援 | DE „Weiterentwicklung“ als Label. Mobile-App nutzt サポート枠 („Support-Anteil“); hier nur dort verwendet, wo DE „Support-Anteil“ sagt (mvEarn). Evtl. einheitlich サポート枠 besser. |
| L.about[3] | 「スマート アプリ コントロール」 | Offizieller Name in der japanischen Windows-Oberflaeche statt „Smart App Control“ – bitte bestaetigen. |
| installBody (Go) | 「許可」 | Beschriftung des Firewall-Buttons in japanischem Windows angenommen als 許可 (DE „Zulassen“). |
| uninstallFailed (Go) | 「アプリと機能」 | Japanischer Windows-Name fuer „Apps & Features“ angenommen. |
| L.nodeSubData, L.mvEarnPartial | (ohne fuehrendes Leerzeichen) | DE beginnt mit Leerzeichen zum Anhaengen; im Japanischen keine Leerzeichen zwischen Saetzen, daher entfernt. |
| L.mvSecs/mvMins/mvAgo/mvAxisFrom, Go minerOffline | 秒 / 分 / 時間 | Statt „s“/„min“/„h“ japanische Zeiteinheiten wie in der Mobile-App; H/s, GB, ms, % unveraendert. „%“ ohne Leerzeichen (0.42%) wie Mobile-App. |
| L.mvThreads | 毎秒ハッシュ数 · ${b}スレッド中${a} | Reihenfolge „b中a“ (Mobile-Muster コア中) – Lesbarkeit pruefen. |
| L.mvShares | 承認 · ${r}件拒否 / 承認、拒否なし | Steht hinter einer grossen Zahl; ob „承認“ allein als Nachsatz natuerlich wirkt. |
| L.mineSub | 報酬は入力したCoreアドレスへ支払われます | DE „deine Core-Adresse“; あなた bewusst vermieden (Mobile-Tonalitaet). |
| L.net[0], Go installBody/crashHint/diskFullHint | CoreStarter自体は / CoreStarterは | DE „Der Starter“ – voller Produktname statt Kurzform „Starter“. |
| syncState (Go) | アカウント状態を読み込み中 | DE „Kontostände“ (Account-State der Chain); evtl. 残高データ verstaendlicher. |
| log.home | それまでは${f} | DE „vorher ${f}“ (vorheriger Server) – Formulierung pruefen. |
| crashHint (Go) | アイコンを右クリック → ログフォルダーを開く | DE „Log öffnen“; an den tatsaechlichen Menuepunkt menuOpenLog („Log-Ordner öffnen“) angeglichen. |
| windowFailed (Go) | …技術的な詳細は次の場所にあります：+ diag | Satzbau umgestellt, damit der Pfad am Ende steht (DE: „stehen in X.“). |
| peers (Go) | %d個のノードに接続中 | Plural-switch entfernt (n==0 → ""), Zaehlwort 個 fuer Nodes pruefen. |

## CoreStarter – Dutch (nl): items for native-speaker review

Terminology follows core-mobile-miner values-nl (je-form, pool, share(s), minen, accu, rekenkracht, vermogen, steunaandeel, opbrengst, uitbetaling, thuisserver).

| # | Key | Dutch text | Why unsure |
|---|---|---|---|
| 1 | all percentages (supportTitle, notes, mvSupportOn, syncBlocks, power …) | `0,42%`, `25%`, `${pct}%`, `%d%%` | German source has a space before %; written without space to match the mobile app's Dutch convention. Revert if the space should stay. |
| 2 | mvSupportL, log.supStart/supDone/supSkip | “Verdere ontwikkeling” | German uses “Weiterentwicklung” as the label; the mobile app uses “Steunaandeel” for the same thing. Kept the literal meaning; “Steunaandeel” would be more consistent with the app. |
| 3 | crashHint (Go) | “… → Logmap openen” | German says “Log öffnen”, but the tray item is “Log-Ordner öffnen”; used the actual menu label. |
| 4 | menuUninstall / uninstallConfirm / uninstallDone | “Verwijderen”, “CoreStarter is verwijderd” | “Verwijderen” is the Windows-NL term for uninstall but is also used for “delete data” (uninstallKeepData). Alternative: “De-installeren”. |
| 5 | uninstallFailed | “Apps en onderdelen” | Must match the Dutch Windows settings page name exactly (Windows 10: “Apps en onderdelen”; Windows 11 differs: “Geïnstalleerde apps”). |
| 6 | noAdmin | “Als administrator uitvoeren” | Should match the Dutch Windows context-menu label; believed correct, not verified. |
| 7 | installBody | “Toestaan” | Should match the button text of the Dutch Windows Firewall dialog (believed “Toegang toestaan”). |
| 8 | synced (Go) | “Helemaal bij · blok %s” | Idiomatic for “up to date”, but informal; alternative “Up-to-date”/“Bijgewerkt”. |
| 9 | syncState (Go) | “Laadt saldi (%s items)” | “Kontostände” (account state) rendered as “saldi”; technically it is the account state trie, “accountstatus” possible. |
| 10 | mvNone | “nog geen in deze sessie” | “Lauf” (run) rendered as “sessie”. |
| 11 | arm64 | “(niet getest)” | Fine, just flagging tone of “geëmuleerd”. |
| 12 | eyebrowInstall / setupNeeded | “Installatie”, “Installatie niet afgerond” | German “Einrichtung” = setup/configuration; “Instellen” might fit better than “Installatie”. |
| 13 | earnings, mvEarnPartial | “poolregio’s” | Uses typographic apostrophe ’ (mobile app uses straight ' due to XML escaping). |
| 14 | mvThreadAvg, mvDiff | “gem.” for Ø | Ø replaced with the Dutch abbreviation “gem.” (gemiddeld); German kept the symbol. |

## CoreStarter – Romanian (ro): items for native-speaker review

Machine-drafted. Terms aligned with core-mobile-miner values-ro (pool, share(-uri), putere de calcul, cota de susținere, plată, sold, jurnal, informal "tu").

| Key | Romanian text | Why unsure |
|---|---|---|
| (all, node) | „nod” / „noduri” | Used the Romanian word for node; some crypto users just say „node”. |
| (all, %) | „0,42 %”, „${pct} %” (space before %) | Kept the German spacing; the mobile app writes „0,42%” without a space. Pick one convention. |
| mvSupportL, log.supStart/supDone/supSkip | „Cota de susținere” | German says „Weiterentwicklung“ (development); used the mobile app's label for the support share instead, for consistency. Alternative: „Dezvoltare”. |
| powerAll, mvThreadIdle | „Tot”, „putere sub „Tot”” | Copied from the mobile app; „Toate” (all threads) may read better. Nested „…” quotes in mvThreadIdle look odd. |
| mvThrottled, minerRunning | „limitat cât timp nodul se sincronizează” | „gedrosselt“: „limitat” vs. „încetinit” / „redus”. |
| mvThreads, mvThreadsL, log.* | „thread” / „thread-uri” | Anglicism kept as in the mobile app; „fir de execuție” is the formal term. |
| openLogs, mvLogTitle, menuOpenLog | „Jurnale”, „Jurnal (miner.log)” | „Logs“ → „Jurnale”; some users expect „Loguri”. |
| crashHint | „clic dreapta pe pictogramă → Deschide folderul de jurnale” | German says „Log öffnen“; used the actual tray menu label instead so it matches the menu. |
| noAdmin | „Executare ca administrator” | Should match the Romanian Windows context-menu label exactly; please verify on a Romanian Windows. |
| uninstallFailed | „Aplicații și caracteristici” | Romanian Windows name for „Apps & Features“; verify. |
| installBody | alege „Permite” | Romanian Windows firewall dialog button label may be „Permitere acces”; verify. |
| autoSub | „după autentificare” | „nach dem Anmelden“ (Windows sign-in); „după conectare” also possible. |
| eyebrowInstall | „Configurare” | „Einrichtung“; alternative „Instalare”. |
| log.dropNoConn | „Share eliminat: nicio conexiune la pool” | „verworfen“; the mobile app uses „Share renunțat”, which seems ungrammatical – „eliminat” or „abandonat” proposed. |
| log.resumed | „Reluat după o pauză de ${d}” | ${d} is a raw duration from the English log (e.g. „5m0s”); reads fine only if short. |
| failed, crashloop, crashHint | „s-a blocat” | „abgestürzt“; „s-a prăbușit” is an alternative. |
| catchingUp, syncState, log.summary, log.answersLost | „blocuri rămase: %s”, „intrări: %s” | Colon form chosen to avoid „de” plural on pre-formatted number strings. Check that it reads naturally. |

Plural handling: JS functions use inline checks (1 → singular, 0/2–19 → plural, 20+ and n%100 ∈ {0, 20–99} → „de” + plural). Go uses a helper `roCount` in texts_ro.go (top-level func, name must not collide in package main).

17 items. The 3 Windows UI labels (noAdmin, uninstallFailed, installBody) are the most important to verify.


## Nachträge 21.10.2026 (nach Neuling-Test, von Claude formuliert)

Neu in allen Sprachen, ebenfalls ungeprüft: Hinweis zur Pool-Gebühr (letzter Punkt der Hinweise), beruhigter Hitzeschutz-Hinweis, Beschriftung "Noch im Pool" (JA プール内（未払い）, NL Nog in de pool, RO Încă în pool) und die Ertragszeile in den Einstellungen.

## Nachträge v0.7 (21.10.2026): Adress-Hilfe, „Ist das sicher?“, Aufhören, Klartext-Zeile



| Sprache | Key | Text | Warum unsicher |
|---|---|---|---|
| ja | stopTitle | やめるには | „Aufhören“ als Überschrift; Alternative 「終了・アンインストール」 wäre sachlicher. |
| ja | supportSub (letzter Satz) | かかるのはこの計算時間だけです。 | Bewusst ohne „お金“ (Strom kostet ja, siehe safe[4]); prüfen, ob es natürlich klingt. |
| ja | plainDay | 1日あたり約${x} XCB | „≈“ durch 約 ersetzt; mvEarnUnit nutzt „XCB／日“ – Stilfrage. |
| ja | safe[1] | …いつでも一時停止や削除ができます。 | Subjekt (CoreStarter) implizit; prüfen ob eindeutig. |
| nl | safe[5] / stopSub | minerweergave · „Verwijderen haalt het programma weg“ | „minerweergave“ abgeleitet von „Liveweergave van de miner“ (Tray-Menü: „Miner tonen …“); Verwijderen = Tray-Term für Deinstallieren. |
| nl | plainStarted | Draait. De eerste share komt zo. | „komt zo“ umgangssprachlich – evtl. „komt zo meteen“. |
| ro | quitBtn / stopSub / safe[5] | Ieși din CoreStarter · Ieșirea · ieșirea | An Tray-Menü „Ieșire“ angelehnt; „Închide CoreStarter“ wäre üblicher, kollidiert aber mit dem Fenster-Button „Închide“. |
| ro | stopTitle | Oprire | „Aufhören“ – Alternative „Renunțare“ / „Oprire și dezinstalare“. |
| ro | safe[4] | Nu costă bani, dar costă curent | „curent“ umgangssprachlich für Strom; ggf. „energie electrică“. |
| ro | plainStarted | Primul share vine imediat. | „share“ als Fremdwort maskulin behandelt (wie „Ultimul share“ im Block). |

## macOS-Texte (23.10.2026, maschinell, ungeprüft)

Alle JA/NL/RO-Texte in `texts_darwin.go` (`macTexts`) und im `MX`-Block von `ui/index.html` sind von Claude entworfen und von niemandem mit Muttersprache geprüft. Die Menübegriffe (Bearbeiten, Kopieren, Im Dock ablegen …) folgen den üblichen macOS-Bezeichnungen der jeweiligen Sprache, aber nicht verifiziert gegen ein lokalisiertes macOS.

| Sprache | Key | Text | Warum unsicher |
|---|---|---|---|
| ja/nl/ro | macTexts.menu | z. B. しまう / Minimaliseer / Minimizează | Ob das genau der Menüname in macOS dieser Sprache ist. |
| ja/nl/ro | moveToApps, Ordnername | 「アプリケーション」 / Apps / Aplicații | Name des Programme-Ordners im lokalisierten Finder. |
| ja/nl/ro | minerHot, mvState paused-heat, log hotOn/hotOff | z. B. Macが高温 / Mac te warm / Mac prea fierbinte | Neu (Hitzepause), Tonfall prüfen. |
| ja/nl/ro | minerHot Grundtext (Linux/Windows, texts_*.go) | 高温 / te warm / prea fierbinte | Neu 27.10., ohne "Mac"; Tonfall wie oben. |
| ja/nl/ro | NSLocalNetworkUsageDescription (`scripts/build-mac.sh`, `localnet`) | Begründung im macOS-Dialog „Lokales Netzwerk“ | Neu, von Claude entworfen; DE von Claude. |

## Mac-Anleitung `packaging/mac/guide.html` (JA/NL/RO, 23.10.2026)

Die Beschriftungen von macOS sind angenommen, nicht auf einem Mac in der jeweiligen Sprache gesehen. Bitte am echten Dialog prüfen:

| Sprache | Annahme | Stelle |
|---|---|---|
| ja | 「“CoreStarter”は開かれませんでした」, 「完了」, 「ゴミ箱に入れる」, 「このまま開く」, システム設定 → プライバシーとセキュリティ, 「置き換える」, 「許可」 | Gatekeeper-Dialog, Systemeinstellungen, Finder, Lokales Netzwerk |
| nl | ‘CoreStarter’ niet geopend, ‘Gereed’, ‘Verplaats naar prullenmand’, ‘Open toch’, Systeeminstellingen → Privacy en beveiliging, ‘Vervang’, ‘Sta toe’ | dito |
| ro | „CoreStarter” nu a fost deschis, „Gata”, „Mută în coș”, „Deschide oricum”, Configurări sistem → Confidențialitate și securitate, „Înlocuiește”, „Permite” | dito |

DE (an Bildschirmfotos eines deutschen macOS geprüft) und EN (macOS-Standardtexte) gelten als sicher.

## Zustimmungskasten (28.10.2026, CONSENT in `ui/index.html`, `installBody`/`minerConsent` in `texts_*.go`)

Die Texte sind rechtliche Hinweise: Bedeutung vor Eleganz. Eine Aenderung nach dem Release heisst neue Textversion (`consent.go`), alle werden erneut gefragt.

| Sprache | Key | Text | Warum unsicher |
|---|---|---|---|
| ja | CONSENT.mineEarn | マイニングしたXCBには課税される場合があります | DE „Geminte XCB können steuerpflichtig sein“; 課税される場合がある als neutrale Steuerformel gewaehlt. |
| ja | CONSENT.mineLoad | 電気代はご自身の負担です | Hoeflichkeitsform ご自身 im sonst neutralen Ton (あなた vermieden); pruefen, ob zu foermlich. |
| ja | CONSENT.agree | 注意事項を読み、同意します。 | Haekchen-Satz; 注意事項 fuer „Hinweise“ (auch im Ersatzdialog). |
| ja | CONSENT.mineShare | 計算時間の0.42%を今後の開発（TMMAC）の支援に充てます | Wortgleich mit supportTitle; „fest eingebaut“ als 組み込み. |
| ja | installBody | 「はい」を選ぶと… | Die Knoepfe der MessageBox kommen von Windows (Systemsprache), nicht von der App-Sprache. |
| nl | CONSENT.mineEarn | Met minen verkregen XCB kunnen belastbaar zijn | Umschreibung statt „gemijnde/geminede XCB“ (Partizip im NL unklar). |
| nl | CONSENT.agree / needAgree | Ik heb de informatie gelezen en ga akkoord. | „Hinweise“ als „informatie“; Alternative „aanwijzingen“. |
| nl | CONSENT.nodeDisk | …in beslag, en dat groeit, plus… | Satzbau holprig? Alternative „…opslag in beslag (en dat wordt meer)…“. |
| ro | CONSENT.mineEarn | XCB minați pot fi impozabili | Genus von XCB (m. Pl.) wie in CorePulse; pruefen. |
| ro | CONSENT.mineShare | Această cotă este integrată fix | „fest eingebaut“; Alternative „este integrată permanent“. |
| ro | CONSENT.nodeAuto | „${title}” este activat | Titel ist ein Verb („Pornește…“); „este activat“ evtl. „este activă (opțiunea)“. |
| alle | minerConsent (Taskleiste) | „… (Einstellungen …)“ | Muss zum Menuepunkt menuSettings passen. |
