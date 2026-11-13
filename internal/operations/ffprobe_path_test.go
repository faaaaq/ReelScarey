package operations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RS-0012: the System check must honor REELSCAREY_FFPROBE_PATH exactly like
// the import and media-analysis subsystems, and say so when the variable is
// set but does not name an executable.
func TestFFprobeCheckHonorsCustomPath(t *testing.T) {
	ctx := context.Background()

	// A working custom binary: the check passes and describes the variable.
	// The running test binary is a guaranteed-executable file on every
	// platform, so LookPath succeeds for the absolute path.
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("REELSCAREY_FFPROBE_PATH", binary)
	s, _, _ := fixture(t)
	report, e := s.Run(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range report.Checks {
		if c.ID != "ffprobe" {
			continue
		}
		if c.State != "ready" {
			t.Fatalf("custom ffprobe path was not honored: %+v", c)
		}
		if !strings.Contains(c.Message, "FFprobe was found") {
			t.Fatalf("unexpected message: %q", c.Message)
		}
	}

	// A custom path that does not exist must fail the check with actionable
	// advice instead of silently probing the default name.
	t.Setenv("REELSCAREY_FFPROBE_PATH", filepath.Join(filepath.Dir(binary), "missing-ffprobe"))
	s2, _, _ := fixture(t)
	report2, e := s2.Run(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range report2.Checks {
		if c.ID != "ffprobe" {
			continue
		}
		if c.State == "ready" {
			t.Fatal("a missing custom ffprobe binary passed the check")
		}
		if !strings.Contains(c.Message, "REELSCAREY_FFPROBE_PATH") {
			t.Fatalf("message does not mention the variable: %q", c.Message)
		}
	}
}
