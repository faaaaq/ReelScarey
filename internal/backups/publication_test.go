package backups

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type pausedRecord struct {
	archive         Archive
	started, resume chan struct{}
}

func (v pausedRecord) MarshalJSON() ([]byte, error) {
	close(v.started)
	<-v.resume
	return json.Marshal(v.archive)
}

func TestArchiveListNeverSeesUnfinishedPublication(t *testing.T) {
	s := &Service{dir: t.TempDir()}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a := Archive{ID: strings.Repeat("a", 32), Kind: "manual", Created: 1, Size: 3, SHA: strings.Repeat("b", 64)}
	v := pausedRecord{a, make(chan struct{}), make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- publishRecord(root, a.ID+".json", v) }()
	<-v.started
	items, listErr := s.List()
	close(v.resume)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if listErr != nil || len(items) != 0 {
		t.Fatal("unfinished record exposed", items, listErr)
	}
	items, err = s.List()
	if err != nil || len(items) != 1 || items[0].SHA != a.SHA {
		t.Fatal(items, err)
	}
	changed := a
	changed.SHA = strings.Repeat("c", 64)
	if err = publishRecord(root, a.ID+".json", changed); err == nil {
		t.Fatal("overwrote existing record")
	}
	items, err = s.List()
	if err != nil || len(items) != 1 || items[0].SHA != a.SHA {
		t.Fatal("published record changed", items, err)
	}
}
