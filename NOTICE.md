# CoreStarter — Notice

Copyright (C) 2026 TMMAC (tellmemoreaboutcore.com)

"TMMAC" in this document means the holder of the rights in this program who publishes it under
the name TMMAC – Tell Me More About Core, and its legal successors. Contact and address: legal
notice (Impressum) at tellmemoreaboutcore.com.

This program is free software: you can redistribute it and/or modify it under the terms of the
GNU General Public License version 3 (see `LICENSE`), together with the additional terms below
as permitted by section 7 of that license. The complete source of every release, tagged by
version, is at github.com/tmmac-core/corestarter.

This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.

SPDX-License-Identifier: GPL-3.0-only

The miner (pool client, pool ranking, address check, support share, hash self-test) is based on
CorePulse by TMMAC (tellmemoreaboutcore.com), under the same license and additional terms.

## Additional terms (GPL-3.0 section 7)

1. **Attribution (section 7 b).** Any work that contains this program or a modified version of it
   must preserve the following author attribution in its Appropriate Legal Notices, shown where
   those notices normally appear (for example an "About" or "Licenses" screen, or the licence
   folder the program opens):
   > Based on CoreStarter by TMMAC (tellmemoreaboutcore.com)
2. **Origin (section 7 c).** The origin of this program must not be misrepresented. Modified
   versions must be marked in reasonable ways as different from the original version, for example
   by a different name or by the words "modified version" in the window title and in any store or
   download listing, and must not be presented as published, endorsed or supported by TMMAC.
3. **Publicity (section 7 d).** The names "TMMAC" and "Tell Me More About Core" may not be used
   to promote products derived from this program without prior written permission.
4. **Trademarks (section 7 e).** This license grants no rights under trademark law for "TMMAC",
   "Tell Me More About Core", "CoreStarter" or the //C logo. See `TRADEMARKS.md`. A modified
   version distributed to others therefore needs its own name and icon.
5. **Liability (section 7 a).** Where sections 15 and 16 are not effective under applicable law,
   in particular German law, the following applies instead: TMMAC is liable without limitation for
   intent and gross negligence, for injury to life, body or health, and wherever liability is
   mandatory by law, in particular under the German Product Liability Act (Produkthaftungsgesetz).
   For slight negligence TMMAC is liable only for breach of an obligation whose fulfilment is
   essential for the proper use of the program and on which the user may regularly rely, and only
   for the foreseeable damage typical for this kind of software. Otherwise liability is excluded.
   This term never extends liability beyond sections 15 and 16 where those are effective.

## Kurz auf Deutsch

Jeder darf das Programm nutzen, prüfen, ändern und weitergeben. Wer es weitergibt oder darauf
aufbaut, muss den vollständigen Quellcode unter derselben Lizenz offenlegen, sichtbar
„Basiert auf CoreStarter von TMMAC (tellmemoreaboutcore.com)“ anzeigen, die Änderung
kenntlich machen und darf nicht so tun, als käme das Programm von TMMAC. Name und Logo sind
nicht mitlizenziert, ein Nachbau braucht deshalb einen eigenen Namen. TMMAC haftet unbeschränkt
bei Vorsatz, grober Fahrlässigkeit, Schäden an Leben und Gesundheit und nach dem
Produkthaftungsgesetz, bei leichter Fahrlässigkeit nur für wesentliche Pflichten und den
typischen, vorhersehbaren Schaden.

## Bundled programs and components

They keep their own licenses. The texts lie in the program's licence folder (button "Licenses"
in the window); in the source, the fonts and RandomY bring theirs in `third_party/`, the others
are fetched by the build scripts.

- **gocore**, the official Core Blockchain node, unchanged release binary, run as a separate
  program. The LICENSE file of go-core says CORE License v1.0 (`LICENSE-gocore.txt`), its
  file headers say GPL-3.0-or-later for the command and LGPL-3.0-or-later for the library
  (texts: `LICENSE-core-starter.txt` is the GPL-3.0, `LICENSE-gocore-LGPL3.txt`). Its source
  with all Go modules is in `third-party-sources-<version>.tar` (below).
- **RandomY** (in the miner): BSD-3-Clause.
- **Go** standard library and the Go modules compiled in (`LICENSE-go*.txt`, written by
  `scripts/go-licenses.sh`): Go and golang.org/x/sys (BSD-3-Clause), fyne.io/systray
  (Apache-2.0); Windows: go-ole, go-winio, go-webview2 (MIT) and its loader package (ISC, a Go
  port that replaces Microsoft's WebView2Loader.dll); Linux: godbus (BSD-2-Clause).
- **Fonts** Barlow, Manrope, Spline Sans Mono: SIL OFL 1.1.
- **Windows:** GCC runtime (GPL-3.0 with GCC Runtime Library Exception), winpthreads (MIT-style);
  the miner links parts of the MinGW-w64 runtime statically (`LICENSE-mingw-w64-runtime.txt`).
- **Linux:** bundled glibc (LGPL-2.1) and GCC runtime (GPL-3.0 with GCC Runtime Library
  Exception) for older distributions.

The unchanged source of gocore, RandomY, the GCC runtime, winpthreads and glibc lies next to each release as
`third-party-sources-<version>.tar` (github.com/tmmac-core/corestarter, Releases).

## Core marks

The program's icons contain the official Core mark, the window shows the XCB mark. Both belong
to their owner and are used with permission. They are not covered by the GPL or by these terms;
a modified version needs its owner's permission to use them (see `TRADEMARKS.md`).
