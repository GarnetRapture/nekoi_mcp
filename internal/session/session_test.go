package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkJudgedIsIdempotent(t *testing.T) {
	st := &State{}
	if !st.MarkJudged("a") {
		t.Fatal("first mark reported the signature as already judged")
	}
	if st.MarkJudged("a") {
		t.Fatal("second mark reported the signature as fresh")
	}
	if !st.Judged("a") {
		t.Fatal("Judged did not see the recorded signature")
	}
	if st.Judged("b") {
		t.Fatal("Judged reported an unrecorded signature")
	}
}

func TestMarkJudgedRejectsEmpty(t *testing.T) {
	st := &State{}
	if st.MarkJudged("") {
		t.Fatal("an empty signature was recorded")
	}
	if len(st.JudgedThoughts) != 0 {
		t.Fatalf("ring holds %d entries after an empty mark", len(st.JudgedThoughts))
	}
}

func TestJudgedRingIsBounded(t *testing.T) {
	st := &State{}
	for i := 0; i < JudgedKeep+50; i++ {
		st.MarkJudged(string(rune('a'+i%26)) + string(rune('0'+i/26%10)) + string(rune(i)))
	}
	if len(st.JudgedThoughts) != JudgedKeep {
		t.Fatalf("ring holds %d entries, want %d", len(st.JudgedThoughts), JudgedKeep)
	}
}

func TestMarkReportedIsSeparateRing(t *testing.T) {
	st := &State{}
	st.MarkJudged("x")
	if !st.MarkReported("x") {
		t.Fatal("the reported ring reused the thinking ring")
	}
	if st.MarkReported("x") {
		t.Fatal("the same reply block was accepted twice")
	}
}

func TestStoreRoundTripKeepsPendingFlag(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	st := s.Load("sess-1")
	st.PendingBlock = true
	st.PendingReason = "flagged"
	st.MarkJudged("sig-1")
	if err := s.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := s.Load("sess-1")
	if !back.PendingBlock || back.PendingReason != "flagged" {
		t.Fatalf("pending state lost: %+v", back)
	}
	if !back.Judged("sig-1") {
		t.Fatal("judged ring did not survive the round trip")
	}
}

func TestStorePathRejectsSeparators(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	st := s.Load("../escape/id")
	if err := s.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("wrote %d entries, want 1", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("unexpected file %q", entries[0].Name())
	}
}
