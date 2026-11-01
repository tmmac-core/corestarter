package main

import (
	"embed"
	"encoding/base64"
	"fmt"
	"strings"
)

// The window's fonts (SIL OFL 1.1, licence texts in third_party/fonts), the
// same as the Core Mobile Miner's, cut down to Latin + the Romanian letters.
// NavigateToString pages cannot load files, so they go in as data: URLs.
//
//go:embed ui/fonts/*.woff2
var uiFonts embed.FS

var uiFontFaces = []struct {
	family, file string
	weight       int
}{
	{"Barlow", "display-800", 800},
	{"Manrope", "text-400", 400},
	{"Manrope", "text-500", 500},
	{"Manrope", "text-600", 600},
	{"Spline Sans Mono", "mono-400", 400},
	{"Spline Sans Mono", "mono-700", 700},
}

const uiFontSlot = "/*@font-face*/"

// withFonts puts the @font-face rules into the page's style sheet.
func withFonts(page string) string {
	var b strings.Builder
	for _, f := range uiFontFaces {
		data, err := uiFonts.ReadFile("ui/fonts/" + f.file + ".woff2")
		if err != nil {
			continue // a missing font falls back to the system fonts in the CSS
		}
		fmt.Fprintf(&b, "@font-face { font-family: %q; font-weight: %d; font-style: normal; font-display: block; src: url(data:font/woff2;base64,%s) format(\"woff2\"); }\n",
			f.family, f.weight, base64.StdEncoding.EncodeToString(data))
	}
	return strings.Replace(page, uiFontSlot, b.String(), 1)
}
