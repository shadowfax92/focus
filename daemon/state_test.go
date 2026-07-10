package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveStateOmitsZeroTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.json")
	if err := SaveState(path, State{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"set_at", "reminder_at"} {
		if strings.Contains(string(b), field) {
			t.Fatalf("zero %s should be omitted: %s", field, b)
		}
	}
}
