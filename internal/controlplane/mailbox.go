package controlplane

import (
	"sync"

	"github.com/cedar2025/xboard-node/internal/model"
)

// MailboxState is the coalesced snapshot drained from a NodeMailbox.
type MailboxState struct {
	NeedsUsersMD5Pull   bool
	ExpectedUsersMD5    string
	ExpectedUserCount   string
}

// NodeMailbox buffers WS checksum events for machine-mode node services.
type NodeMailbox struct {
	mu       sync.Mutex
	ready    bool
	notifyCh chan struct{}

	pendingUsersMD5   string
	pendingUserCount  string
	needsUsersMD5Pull bool
}

func NewNodeMailbox() *NodeMailbox {
	return &NodeMailbox{notifyCh: make(chan struct{}, 1)}
}

func (m *NodeMailbox) MarkReady() {
	m.mu.Lock()
	m.ready = true
	m.mu.Unlock()
	m.notify()
}

// SeedBaseline is kept for API compatibility; WS only delivers user checksums now.
func (m *NodeMailbox) SeedBaseline(users []model.UserSpec, config *model.NodeSpec) {}

func (m *NodeMailbox) Apply(event Event) {
	m.mu.Lock()
	changed := false

	if event.Type == EventSyncUsersMD5 && event.UsersMD5 != "" {
		m.pendingUsersMD5 = event.UsersMD5
		m.pendingUserCount = event.UserCount
		m.needsUsersMD5Pull = true
		changed = true
	}

	m.mu.Unlock()

	if changed {
		m.notify()
	}
}

func (m *NodeMailbox) notify() {
	select {
	case m.notifyCh <- struct{}{}:
	default:
	}
}

func (m *NodeMailbox) NotifyCh() <-chan struct{} { return m.notifyCh }

func (m *NodeMailbox) DrainIfReady() MailboxState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready {
		return MailboxState{}
	}
	state := MailboxState{}
	if m.needsUsersMD5Pull {
		state.NeedsUsersMD5Pull = true
		state.ExpectedUsersMD5 = m.pendingUsersMD5
		state.ExpectedUserCount = m.pendingUserCount
		m.needsUsersMD5Pull = false
	}
	return state
}
