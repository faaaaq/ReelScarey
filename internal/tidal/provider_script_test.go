package tidal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestProviderScriptLoginLifecycle executes the real provider against an
// offline tidaler stub. It catches the former daemon-thread failure where
// login-begin returned 202 but its process exited before writing a device
// link or even creating the login state file.
func TestProviderScriptLoginLifecycle(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
		if err != nil {
			t.Skip("Python is not installed; the Docker acceptance image runs this provider check")
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "test_tidal_provider.py")
	if _, err := os.Stat(script); err != nil {
		if os.IsNotExist(err) {
			t.Skip("provider sources are not copied into the backend-only Docker build stage")
		}
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("provider lifecycle test timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("provider lifecycle test failed: %v\n%s", err, output)
	}
}
