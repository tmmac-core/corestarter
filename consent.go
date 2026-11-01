package main

import (
	_ "embed"
	"errors"
	"sync/atomic"
	"time"
)

// Consent versions: raise one whenever its text changes (CONSENT in
// ui/index.html, installBody in texts*.go, NOTICE.md); everyone is then asked again.
// TestConsentVersion keeps a hash of the texts and fails until the version and
// the hash are updated together.
const (
	consentNodeVersion = 4 // beta, licence and liability, disk, P2P, autostart
	consentMineVersion = 3 // load and power costs, support share, earnings and tax, pool data
)

// consentMark records which text version was accepted, and when. It stays on
// this PC (settings.json) and is never sent anywhere.
type consentMark struct {
	Version int       `json:"version"`
	At      time.Time `json:"at"`
}

// Consent holds the accepted versions. Only Go sets it: the page can say
// "agreed", never which version or when.
type Consent struct {
	Node *consentMark `json:"node,omitempty"`
	Mine *consentMark `json:"mine,omitempty"`
}

func (c *consentMark) covers(version int) bool { return c != nil && c.Version >= version }

func (s Settings) nodeConsented() bool { return s.Consent.Node.covers(consentNodeVersion) }
func (s Settings) mineConsented() bool { return s.Consent.Mine.covers(consentMineVersion) }

// agree stamps what the user just ticked with the current versions.
func (c *Consent) agree(node, mine bool, now time.Time) {
	now = now.Truncate(time.Second)
	if node {
		c.Node = &consentMark{Version: consentNodeVersion, At: now}
	}
	if mine {
		c.Mine = &consentMark{Version: consentMineVersion, At: now}
	}
}

var errConsent = errors.New("the notes were not accepted")

// consentProblem: the first save (nothing on record, prev == nil) needs the
// current consent, mining its own. Later a save only needs it when it switches
// something on (node, mining, autostart): switching off or changing the power
// always works, also for someone who has not accepted a newer text yet. A saved
// Mine without the current mining consent is harmless, the tray does not start
// the miner then (minerConfigFrom).
func (s Settings) consentProblem(prev *Settings) error {
	if prev != nil && !(s.Node && !prev.Node) && !(s.Mine && !prev.Mine) && !(s.Autostart && !prev.Autostart) {
		return nil
	}
	if !s.nodeConsented() || (s.Mine && !s.mineConsented()) {
		return errConsent
	}
	return nil
}

// autostartProblem: switching autostart on later shows its note again (the
// page lists nodeAuto), so the save must carry that tick (agree.node) even
// when the node consent is current; the tick then re-stamps Consent.Node.
// The first save (prev == nil) needs the node tick anyway (consentProblem).
func (s Settings) autostartProblem(prev *Settings, agreedNode bool) error {
	if prev != nil && !prev.Autostart && s.Autostart && !agreedNode {
		return errConsent
	}
	return nil
}

// savedSettings: the settings on record (nil if none or damaged); their
// consent is the only one Go trusts, never the page's copy.
func savedSettings(path string) *Settings {
	s, err := LoadSettings(path)
	if err != nil {
		return nil
	}
	return &s
}

// privacyURL: the apps section of the privacy policy on the publisher's website.
func privacyURL(lang string) string {
	if lang == "de" {
		return "https://tellmemoreaboutcore.com/de/datenschutz/#apps"
	}
	return "https://tellmemoreaboutcore.com/privacy/#apps"
}

// sourceURL: the public repository with the source of every release (GPL-3.0 section 6).
const sourceURL = "https://github.com/tmmac-core/corestarter"

// siteURL: the publisher's pages the window links to (German pages for German,
// English otherwise). Fixed addresses only, the page cannot pass its own URL.
// "feedback" is the contact form in the imprint, covered by the privacy policy.
func siteURL(page, lang string) string {
	const site = "https://tellmemoreaboutcore.com"
	de := lang == "de"
	switch page {
	case "imprint":
		if de {
			return site + "/de/impressum/"
		}
		return site + "/imprint/"
	case "feedback":
		if de {
			return site + "/de/impressum/#contact"
		}
		return site + "/imprint/#contact"
	case "release":
		if de {
			return site + "/de/core-starter/"
		}
		return site + "/core-starter/"
	case "source":
		return sourceURL
	}
	if de {
		return site + "/de/"
	}
	return site + "/"
}

//go:embed NOTICE.md
var noticeText string

// mineAwaitsConsent: mining is switched on, but its consent is missing or
// older than the current text, so the miner stays off and the tray says why.
var mineAwaitsConsent atomic.Bool

// applyMiner gives the miner the saved settings (minerConfigFrom refuses
// without consent) and remembers whether it waits for consent.
func applyMiner(miner *minerSup, s Settings) {
	mineAwaitsConsent.Store(minerAvailable && s.Mine && !s.mineConsented())
	miner.Apply(minerConfigFrom(s))
}
