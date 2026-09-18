package controlplane

import "testing"

func TestNodeMailboxUsersMD5(t *testing.T) {
	mb := NewNodeMailbox()
	mb.MarkReady()

	mb.Apply(Event{Type: EventSyncUsersMD5, UsersMD5: "abc", UserCount: "10"})
	state := mb.DrainIfReady()
	if !state.NeedsUsersMD5Pull {
		t.Fatal("expected md5 pull")
	}
	if state.ExpectedUsersMD5 != "abc" {
		t.Fatalf("md5 = %q", state.ExpectedUsersMD5)
	}
	if state.ExpectedUserCount != "10" {
		t.Fatalf("count = %q", state.ExpectedUserCount)
	}

	// Second drain should be empty until another event arrives.
	empty := mb.DrainIfReady()
	if empty.NeedsUsersMD5Pull {
		t.Fatal("expected no pending md5 after drain")
	}
}
