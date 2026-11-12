package faultcheck

import (
	"strings"
	"testing"
)

func TestReportRecordsOnlyOnceStarted(t *testing.T) {
	recording.Store(false)
	Take()
	Report("before", "x")
	if got := Take(); len(got) != 0 {
		t.Fatalf("Report recorded %v before Start", got)
	}
	Start()
	t.Cleanup(func() { recording.Store(false) })

	Report("after", "boom")
	Report("expected", Expected{Why: "test"})
	got := Take()
	if len(got) != 1 {
		t.Fatalf("recorded %d panics, want 1 (Expected is ignored): %v", len(got), got)
	}
	if got[0].Site != "after" || got[0].Value != "boom" {
		t.Errorf("recorded %+v", got[0])
	}
	if !strings.Contains(got[0].Stack, "TestReportRecordsOnlyOnceStarted") {
		t.Errorf("stack does not name the caller:\n%s", got[0].Stack)
	}
	if again := Take(); len(again) != 0 {
		t.Errorf("Take did not clear: %v", again)
	}
}
