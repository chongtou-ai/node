package singbox

import (
	"net"
	"testing"
)

type stubConn struct {
	net.Conn
	closed bool
}

func (s *stubConn) Close() error {
	s.closed = true
	return nil
}

func TestConnTrackerCloseByUUID(t *testing.T) {
	t.Parallel()

	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1})

	tcp1 := &stubConn{}
	tcp2 := &stubConn{}
	tracker.usersMu.Lock()
	id1 := tracker.nextID()
	tracker.connMap[id1] = tcp1
	tracker.connUUID[id1] = "uuid-1"
	id2 := tracker.nextID()
	tracker.connMap[id2] = tcp2
	tracker.connUUID[id2] = "uuid-1"
	tracker.usersMu.Unlock()

	if got := tracker.CloseByUUID("uuid-1"); got != 2 {
		t.Fatalf("CloseByUUID() = %d, want 2", got)
	}
	if !tcp1.closed || !tcp2.closed {
		t.Fatalf("expected both connections closed, tcp1=%v tcp2=%v", tcp1.closed, tcp2.closed)
	}
}

func TestConnTrackerCloseByUUIDIgnoresOtherUsers(t *testing.T) {
	t.Parallel()

	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1, "uuid-2": 2})

	keep := &stubConn{}
	tracker.usersMu.Lock()
	keepID := tracker.nextID()
	tracker.connMap[keepID] = keep
	tracker.connUUID[keepID] = "uuid-2"
	remove := &stubConn{}
	removeID := tracker.nextID()
	tracker.connMap[removeID] = remove
	tracker.connUUID[removeID] = "uuid-1"
	tracker.usersMu.Unlock()

	if got := tracker.CloseByUUID("uuid-1"); got != 1 {
		t.Fatalf("CloseByUUID() = %d, want 1", got)
	}
	if !remove.closed {
		t.Fatal("expected removed user conn closed")
	}
	if keep.closed {
		t.Fatal("expected other user's conn to remain open")
	}
}
