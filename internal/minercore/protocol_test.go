package minercore

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// A failed query of the pool API shows in the status with the time to the
// next one; a successful one clears it again. Mining is not part of this.
func TestEarningsWatch(t *testing.T) {
	var w EarningsWatch
	sec := int64(time.Second)

	var s MinerStatus
	w.Fill(&s, 0)
	if s.EarningsDown || s.EarningsRetrySecs != 0 {
		t.Fatalf("before any query: %+v", s)
	}

	w.Result(errors.New("HTTP 521"), 600*sec)
	s = MinerStatus{}
	w.Fill(&s, 30*sec)
	if !s.EarningsDown || s.EarningsRetrySecs != 570 {
		t.Fatalf("after a failed query: down=%v retry=%d, want true 570", s.EarningsDown, s.EarningsRetrySecs)
	}
	// Past the planned time (query running): still down, no negative wait.
	s = MinerStatus{}
	w.Fill(&s, 700*sec)
	if !s.EarningsDown || s.EarningsRetrySecs != 0 {
		t.Fatalf("query overdue: down=%v retry=%d", s.EarningsDown, s.EarningsRetrySecs)
	}

	w.Result(nil, 1300*sec)
	s = MinerStatus{}
	w.Fill(&s, 710*sec)
	if s.EarningsDown || s.EarningsRetrySecs != 0 {
		t.Fatalf("after a good query: %+v", s)
	}
	// Both fields stay out of the JSON while everything works.
	b, _ := json.Marshal(&s)
	if strings.Contains(string(b), "earningsDown") || strings.Contains(string(b), "earningsRetrySecs") {
		t.Fatalf("fields in JSON while the API works: %s", b)
	}
}
