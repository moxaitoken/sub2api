package usage

import (
	"testing"
	"time"
)

func TestFullHistoryProbe(t *testing.T) {
	_, _, ok := Window("all", time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("full history period rejected")
	}
	t.Log("full history period accepted")
}
