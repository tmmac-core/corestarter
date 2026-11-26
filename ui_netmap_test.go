package main

import (
	"strings"
	"testing"
)

func TestWorldMapInPage(t *testing.T) {
	if strings.Contains(uiPage, uiWorldMapSlot) {
		t.Fatal("world map slot left in the page")
	}
	for _, want := range []string{"const WORLD_PATH = \"M", "const WORLD_POINTS = {", "DE:[", "mnCard"} {
		if !strings.Contains(uiPage, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Every language has every network text (the page falls back to English).
	for _, key := range []string{"mnTitle:", "mnSub:", "mnMapLabel:", "mnLConn:", "mnHead:", "mnInHead:", "mnInBody:", "mnWasInHead:", "mnWaitBody:", "mnNoInHead:", "mnNoInBody:", "mnPortQ:", "mnPortA:", "mnOffHead:", "mnOffBody:", "mnEmptyHead:", "mnEmptyBody:", "mnTip:", "mnSrc:", "mnMore:", "mnUnplaced:"} {
		if n := strings.Count(uiPage, "\n    "+key); n != 5 {
			t.Errorf("%s in %d languages, want 5", key, n)
		}
	}
}
