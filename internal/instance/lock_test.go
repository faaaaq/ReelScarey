package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExclusiveOwnershipAndSharedAdministration(t *testing.T) {
	dir := t.TempDir()
	a, e := Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if b, e := Acquire(dir); e == nil {
		b.Close()
		t.Fatal("second server obtained config ownership")
	}
	s, e := Guard(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	s2, e := Guard(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := Guard(dir, false); e == nil {
		b.Close()
		t.Fatal("offline maintenance overlapped live administration")
	}
	s.Close()
	s2.Close()
	b, e := Guard(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	b.Close()
	a.Close()
	b, e = Acquire(dir)
	if e != nil {
		t.Fatal("released lock was not reclaimable", e)
	}
	b.Close()
	if e = os.WriteFile(filepath.Join(dir, ".restore-in-progress"), []byte("incomplete"), 0600); e != nil {
		t.Fatal(e)
	}
	if CheckStart(dir) == nil {
		t.Fatal("incomplete restore could start")
	}
	if e = os.Remove(filepath.Join(dir, ".restore-in-progress")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, ".restore-request.json"), []byte("pending"), 0600); e != nil {
		t.Fatal(e)
	}
	if CheckStart(dir) == nil {
		t.Fatal("administration bypassed pending restore")
	}
}
