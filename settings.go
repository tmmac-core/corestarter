package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

// Settings is what the user chose in the window. Only the window writes the
// file; the tray reads it (and is told to reload by an event).
type Settings struct {
	Node      bool    `json:"node"`
	Mine      bool    `json:"mine"`
	Address   string  `json:"address"` // Core address (ICAN) that receives the mining payouts
	Worker    string  `json:"worker"`  // name shown for this PC in the pool statistics
	Power     int     `json:"power"`   // CPU share for mining: 25, 50, 75 or 100
	Autostart bool    `json:"autostart"`
	Lang      string  `json:"lang,omitempty"` // window, tray and dialogs; "" = Windows' language
	Consent   Consent `json:"consent"`        // accepted notes, see consent.go
}

// uiLangs: the languages of window, tray and dialogs (as the Core Mobile Miner).
var uiLangs = []string{"de", "en", "ja", "nl", "ro"}

// DefaultSettings is what the window proposes on first start.
func DefaultSettings() Settings {
	return Settings{Node: true, Power: 50, Worker: minercore.SuggestWorker("pc")}
}

var errNoSettings = errors.New("no settings saved yet")

// Normalize fixes what can be fixed and reports what cannot.
func (s *Settings) Normalize() error {
	switch s.Power {
	case 25, 50, 75, 100:
	default:
		s.Power = 50
	}
	if !slices.Contains(uiLangs, s.Lang) {
		s.Lang = "" // unknown (or a newer version's) language: Windows decides
	}
	s.Worker = minercore.SanitizeWorker(s.Worker)
	if s.Worker == "" {
		s.Worker = minercore.SuggestWorker("pc")
	}
	if s.Address == "" && !s.Mine {
		return nil
	}
	addr, err := minercore.ParseICAN(s.Address)
	if err == nil && addr.Network != minercore.Mainnet {
		err = minercore.ErrNotMainnet
	}
	switch {
	case err == nil:
		s.Address = addr.Address
	case s.Mine:
		return err
	default:
		s.Address = "" // only needed for mining; a bad one must not block the node
	}
	return nil
}

// LoadSettings reads the file. A missing file is errNoSettings; a damaged one
// is an error too, so the tray starts nothing and asks again.
func LoadSettings(path string) (Settings, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, errNoSettings
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, err
	}
	if err := s.Normalize(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// settingsMu serializes this process's writers of settings.json: the window's
// UI thread (setlang) and its goroutines (save, setmine) each read, change and
// write the file, and must not lose each other's change. Held only for the
// file work, never while waiting for the tray.
var settingsMu sync.Mutex

// SaveSettings writes s atomically and keeps fields this version does not know
// (written by a newer version), so a downgrade-then-upgrade loses nothing.
func SaveSettings(path string, s Settings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return saveSettingsLocked(path, s)
}

// updateSettings loads the saved settings, lets change edit them and saves
// them, all under settingsMu. change runs only on loaded settings; it returns
// whether to save. changed reports whether a save was attempted.
func updateSettings(path string, change func(*Settings) (bool, error)) (changed bool, err error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s, err := LoadSettings(path)
	if err != nil {
		return false, err
	}
	save, err := change(&s)
	if err != nil || !save {
		return false, err
	}
	return true, saveSettingsLocked(path, s)
}

func saveSettingsLocked(path string, s Settings) error {
	if err := s.Normalize(); err != nil {
		return err
	}
	merged := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, &merged) // a damaged file is simply replaced
	}
	if merged == nil { // the file said null
		merged = map[string]json.RawMessage{}
	}
	// lang is omitted when "" (automatic): a known old choice must not stay.
	// A language this version does not know (a newer one wrote it) stays.
	var old string
	if json.Unmarshal(merged["lang"], &old) == nil && (old == "" || slices.Contains(uiLangs, old)) {
		delete(merged, "lang")
	}
	ours, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(ours, &fields); err != nil {
		return err
	}
	for k, v := range fields {
		merged[k] = v
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// A file of its own per write (0600, like before): two writers never
	// share a half-written temp file.
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(out)
	if cerr := f.Close(); err == nil {
		err = cerr // closed before the rename: Windows cannot rename an open file
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return renameRetry(tmp, path)
}

// renameRetry replaces path with tmp. On Windows the rename fails while
// another process (the window, a virus scanner) has path open for reading, so
// it tries a few more times.
func renameRetry(tmp, path string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	os.Remove(tmp)
	return err
}
