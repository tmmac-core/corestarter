//go:build !nominer

package main

import "testing"

// The miner only starts with the current mining consent; the tray then says
// why it waits instead of showing nothing.
func TestMinerNeedsConsent(t *testing.T) {
	s := Settings{Node: true, Mine: true, Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "pc-test", Power: 50}
	if minerConfigFrom(s) != nil {
		t.Error("miner config without consent")
	}
	applyMiner(&minerSup{}, s)
	if !mineAwaitsConsent.Load() {
		t.Error("tray does not know that mining waits for consent")
	}
	s.Consent = agreedConsent()
	s.Consent.Mine.Version = consentMineVersion - 1 // a text changed since
	if minerConfigFrom(s) != nil {
		t.Error("miner config with an outdated consent")
	}
	s.Consent = agreedConsent()
	if c := minerConfigFrom(s); c == nil || c.Address != s.Address {
		t.Errorf("miner config with consent: %+v", c)
	}
	s.Mine = false
	applyMiner(&minerSup{}, s)
	if mineAwaitsConsent.Load() {
		t.Error("mining off still waits for consent")
	}
}
