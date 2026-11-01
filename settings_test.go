package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsRoundTripKeepsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := LoadSettings(path); !errors.Is(err, errNoSettings) {
		t.Fatalf("missing file: %v", err)
	}
	os.WriteFile(path, []byte(`{"node":false,"futureField":{"x":1}}`), 0o600)
	s := DefaultSettings()
	s.Mine = true
	s.Address = "  CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963 "
	s.Worker = "My PC"
	s.Power = 33
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"futureField"`) {
		t.Errorf("unknown field lost: %s", raw)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963" || got.Worker != "my-pc" || got.Power != 50 || !got.Mine || !got.Node {
		t.Errorf("normalized wrong: %+v", got)
	}
}

func TestSettingsRejectsBadAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.Mine = true
	s.Address = "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4964"
	if err := SaveSettings(path, s); err == nil {
		t.Fatal("wrong checksum saved")
	}
	s.Address = "ab472120d3abc3b8330a26daf4beb94b1f6a9c7b4963"
	if err := SaveSettings(path, s); err == nil {
		t.Fatal("testnet address saved for mining")
	}
	s.Mine, s.Address = false, ""
	if err := SaveSettings(path, s); err != nil {
		t.Fatalf("node only: %v", err)
	}
	// A bad address with mining off must not block the node; it is dropped.
	s.Mine, s.Address = false, "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4964"
	if err := SaveSettings(path, s); err != nil {
		t.Fatalf("node only with a stale bad address: %v", err)
	}
	if got, err := LoadSettings(path); err != nil || got.Address != "" {
		t.Fatalf("bad address kept: %q %v", got.Address, err)
	}
	os.WriteFile(path, []byte(`{"node":tru`), 0o600)
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("damaged file accepted")
	}
}

// Concurrent load-change-save paths (setlang, setmine, save) lose no change
// and leave no temp file behind.
func TestUpdateSettingsConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveSettings(path, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	langs := []string{"de", "en", "ja", "nl", "ro"}
	done := make(chan error)
	for i := 0; i < 40; i++ {
		go func(i int) {
			var err error
			switch i % 3 {
			case 0:
				_, err = updateSettings(path, func(s *Settings) (bool, error) { s.Lang = langs[i%len(langs)]; return true, nil })
			case 1:
				_, err = updateSettings(path, func(s *Settings) (bool, error) { s.Power = 25 + 25*(i%4); return true, nil })
			default:
				err = SaveSettings(path, DefaultSettings())
			}
			done <- err
		}(i)
	}
	for i := 0; i < 40; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if _, err := LoadSettings(path); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Errorf("files left: %v", ents)
	}
}

// "Auto" (Lang "") is omitted from the JSON; an earlier "de" must not stay.
func TestSettingsLangBackToAuto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.Lang = "de"
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	s.Lang = ""
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lang != "" {
		t.Errorf("lang = %q, want automatic", got.Lang)
	}
}

// A file that says null is loaded as empty settings; saving over it must work.
func TestSettingsSaveOverNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("null"), 0o600)
	if err := SaveSettings(path, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(path); err != nil {
		t.Fatal(err)
	}
}
