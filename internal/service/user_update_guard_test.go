package service

import (
	"context"
	"testing"

	"github.com/cedar2025/xboard-node/internal/model"
)

func TestApplyUserUpdateRefusesAccidentalEmptyWipe(t *testing.T) {
	k := &fakeKernel{running: true}
	s := newTestService(k)
	s.lastUsers = []model.UserSpec{{ID: 1, UUID: "u1"}}
	s.lastUsersMD5 = "non-empty"
	s.lastConfig = &model.NodeSpec{Protocol: "vless"}

	s.applyUserUpdate(context.Background(), nil, "still-non-empty-remote-md5")

	if k.updateCalls != 0 {
		t.Fatalf("UpdateUsers should not run on accidental empty wipe, got %d", k.updateCalls)
	}
	if len(s.lastUsers) != 1 {
		t.Fatalf("local users wiped: %+v", s.lastUsers)
	}
}

func TestApplyUserUpdateAllowsGenuineEmptyList(t *testing.T) {
	k := &fakeKernel{running: true}
	s := newTestService(k)
	s.lastUsers = []model.UserSpec{{ID: 1, UUID: "u1"}}
	s.lastConfig = &model.NodeSpec{Protocol: "vless"}
	emptyMD5 := computeUsersMD5FromSpecs(nil)

	s.applyUserUpdate(context.Background(), nil, emptyMD5)

	if k.updateCalls != 1 {
		t.Fatalf("UpdateUsers calls = %d, want 1", k.updateCalls)
	}
	if len(s.lastUsers) != 0 {
		t.Fatalf("expected empty local users, got %+v", s.lastUsers)
	}
}
