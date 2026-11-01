# Release

Veröffentlicht wird nur im öffentlichen Repo `github.com/tmmac-core/corestarter`, aus einem Tag `v<appVersion>`,
mit allen Dateien aus genau diesem Stand. Die Programme sind noch nicht signiert.

## Jedes Release

1. `appVersion` in `app.go` setzen, Eintrag in `CHANGELOG.md`. Ändert sich ein Zustimmungstext (auch `NOTICE.md`),
   steigt die Version in `consent.go` (der Test `TestConsentVersion` erzwingt das).
2. Prüfungen grün: `go test -race . ./internal/...`, `GOOS=windows go vet .` (mit und ohne `-tags nominer`), die
   Workflows `windows`, `linux` und `runtime-pins` auf dem Release-Commit.
3. Programme aus dem Release-Commit:
   - Windows: Artefakt `build` des Workflows `windows` (`CoreStarter.exe` + `.sha256`).
   - Linux: Artefakt `linux-packages` des Workflows `linux` (`.deb`, `.rpm`, `.tar.gz`).
   - macOS: `./scripts/build-mac.sh` auf einem Mac mit Apple Silicon (`dist/CoreStarter-<v>-mac-arm64.dmg`).
   - Quellen der mitgelieferten Bibliotheken: `./scripts/third-party-sources.sh`
     (`dist/third-party-sources-<v>.tar`).
4. `SHA256SUMS` über alle Dateien des Releases bilden (`shasum -a 256 * > SHA256SUMS`).
5. Tag `v<v>` auf den Release-Commit, als TMMAC:
   `GIT_COMMITTER_NAME=TMMAC GIT_COMMITTER_EMAIL=contact@tellmemoreaboutcore.com git tag -a v<v> -m "CoreStarter <v>"`.
   GitHub Release mit allen Dateien, `SHA256SUMS` und der Release-Notiz (Hinweis auf die fehlende Signatur).
6. Downloadseite (tellmemoreaboutcore.com/core-starter/) auf die neue Version stellen und jede Datei einmal über
   die Seite laden und mit `SHA256SUMS` vergleichen.

Der Wochenlauf `runtime-pins` prüft, ob die für ältere Linux-Systeme mitgelieferte glibc/GCC-Laufzeit noch der
aktuellen Ubuntu-Version entspricht. Schlägt er fehl, ist ein neues Release mit neu gepinnten Dateien nötig; der
Build selbst ist dann nicht kaputt.

## Prüfen eines Downloads

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing
```
