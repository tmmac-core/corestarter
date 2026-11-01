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
5. Tag `v<v>` auf den Release-Commit, GitHub Release mit allen Dateien und `SHA256SUMS` und der Release-Notiz (Hinweis auf die fehlende Signatur).
6. Website: `apps/core-starter.json` mit Version, Datei-URLs und SHA-256 auf beide Docroots, dann `/core-starter/`
   und `/de/core-starter/` prüfen und jede Datei einmal über die Seite herunterladen und mit `SHA256SUMS` vergleichen.

## Prüfen eines Downloads

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing
```
