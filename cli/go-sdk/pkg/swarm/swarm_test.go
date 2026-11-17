package swarm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMentionsAndChannelMentions(t *testing.T) {
	if !Mentions("Hey @verifier, please check", "verifier") {
		t.Fatal("expected exact @verifier match")
	}
	if Mentions("email test@verifier.com", "verifier") {
		t.Fatal("email address must not trigger @mention")
	}
	if !Mentions("Calling @all workers", "worker-1") {
		t.Fatal("@all must match worker-1")
	}
	if !Mentions("Shard update for @worker please", "worker-2") {
		t.Fatal("copy prefix @worker must match worker-2")
	}
	if Mentions("Shard update for @worker-1", "worker-2") {
		t.Fatal("@worker-1 must not match worker-2")
	}

	active := func(ch string) bool { return ch == "api" }
	if !MentionsIn("Look at @#api now", "alice", "main", []string{"api"}, active) {
		t.Fatal("@#api must match member subscribed to api")
	}
	if MentionsIn("Look at @#archived now", "alice", "main", []string{"archived"}, active) {
		t.Fatal("inactive channel must not match")
	}
}

func TestFileStoreCreatePostDedupAndLegacyBridge(t *testing.T) {
	dir := t.TempDir()
	st, err := Create(dir, "Test Swarm", "coord-conv-1", "gemini-4-hydrogen-medium")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	briefMsg, err := st.AddMembers(AddMembersRequest{
		Actor: "coordinator",
		Members: []NewMember{
			{Name: "worker-1", Role: "worker", DesiredModel: "gemini-4-hydrogen-medium", WorkspaceMode: "share"},
		},
		BriefText: "@worker-1 own API",
		RequestID: "req-brief-1",
	})
	if err != nil {
		t.Fatalf("AddMembers failed: %v", err)
	}
	if briefMsg.Seq != 1 || briefMsg.Kind != MessageKindBrief {
		t.Fatalf("unexpected briefMsg: %+v", briefMsg)
	}

	// Duplicate RequestID returns existing seq=1 instead of appending seq=2.
	dup, err := st.Post(PostRequest{
		Author:    "coordinator",
		Text:      "@worker-1 own API",
		Kind:      MessageKindBrief,
		RequestID: "req-brief-1",
	})
	if err != nil || dup.Seq != 1 || st.Head() != 1 {
		t.Fatalf("expected RequestID dedup at seq=1, got dup=%+v head=%d err=%v", dup, st.Head(), err)
	}

	if err := st.Kill([]string{"worker-1"}); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "members.json"))
	if err != nil {
		t.Fatalf("ReadFile members.json failed: %v", err)
	}
	if !containsSubstr(string(raw), `"killed": true`) {
		t.Fatalf("expected legacy killed:true field in members.json, got:\n%s", raw)
	}

	snap, err := InspectDir(dir)
	if err != nil {
		t.Fatalf("InspectDir failed: %v", err)
	}
	if snap.LiveCount != 1 || snap.KilledCount != 1 || snap.HeadSeq != 1 {
		t.Fatalf("unexpected snapshot: live=%d killed=%d head=%d", snap.LiveCount, snap.KilledCount, snap.HeadSeq)
	}
}

func TestWakeEngineAndQuotaPause(t *testing.T) {
	dir := t.TempDir()
	st, err := Create(dir, "Wake Test", "coord-1", "gemini-4-hydrogen-medium")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := st.AddMembers(AddMembersRequest{
		Actor: "coordinator",
		Members: []NewMember{
			{Name: "alice", Role: "worker"},
			{Name: "bob", Role: "worker"},
		},
	}); err != nil {
		t.Fatalf("AddMembers failed: %v", err)
	}

	// Park alice on mentions since seq 0, keep bob working.
	if err := st.UpdateMemberLocked("alice", func(ms *MemberState) {
		ms.WakeMode = WakeModeMentions
		ms.WaitSince = 0
	}); err != nil {
		t.Fatalf("UpdateMemberLocked failed: %v", err)
	}

	we := NewWakeEngine(st, "")
	defer we.Close()

	// Unrelated message should not wake alice.
	if _, err := st.Post(PostRequest{Author: "bob", Text: "just chatting"}); err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	select {
	case d := <-we.Doorbells():
		t.Fatalf("unexpected doorbell on unrelated message: %+v", d)
	case <-time.After(30 * time.Millisecond):
	}

	// Mentioning @alice wakes her.
	if _, err := st.Post(PostRequest{Author: "bob", Text: "hi @alice please review"}); err != nil {
		t.Fatalf("Post mention failed: %v", err)
	}
	select {
	case d := <-we.Doorbells():
		if d.Member != "alice" || d.Seq != 2 || d.Reason != WakeModeMentions {
			t.Fatalf("unexpected doorbell: %+v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for @alice wake doorbell")
	}

	// Verify QuotaPauseTracker sits out until board moves past pause head.
	qp := NewQuotaPauseTracker()
	pauseHead := qp.PauseOnQuota(st, "bob", "gemini-4-hydrogen-medium", "traj-1")
	if pauseHead != 2 || !qp.SitOutForQuota(st, "bob") {
		t.Fatalf("expected bob to sit out at head=2")
	}
	if _, err := st.Post(PostRequest{Author: "alice", Text: "progress"}); err != nil {
		t.Fatalf("Post progress failed: %v", err)
	}
	if qp.SitOutForQuota(st, "bob") {
		t.Fatal("expected bob to resume after board head moved to 3")
	}
}

func containsSubstr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexSubstr(s, sub) >= 0)
}

func indexSubstr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
