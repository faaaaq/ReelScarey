package instance

import (
	"strings"
	"testing"
)

// RS-0019: a plain double-start must be told another server is running, not
// given the offline-maintenance advice; the maintenance guard keeps its own
// wording.
func TestSecondServerMessageNamesTheServer(t *testing.T) {
	dir := t.TempDir()
	a, e := Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	_, e = Acquire(dir)
	if e == nil {
		t.Fatal("second server obtained config ownership")
	}
	if !strings.Contains(e.Error(), "another ReelScarey server") {
		t.Fatalf("second-server message blames the wrong cause: %q", e.Error())
	}
}
