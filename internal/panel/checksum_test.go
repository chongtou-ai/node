package panel

import (
	"testing"
)

func TestComputeUsersMD5(t *testing.T) {
	users := []User{
		{ID: 2, UUID: "b-uuid", SpeedLimit: 10, DeviceLimit: 1, PlanID: 5, UnlimitedTraffic: 0},
		{ID: 1, UUID: "a-uuid", SpeedLimit: 20, DeviceLimit: 2, PlanID: 3, UnlimitedTraffic: 1},
	}

	got := ComputeUsersMD5(users)
	want := ComputeUsersMD5([]User{
		{ID: 1, UUID: "a-uuid", SpeedLimit: 20, DeviceLimit: 2, PlanID: 3, UnlimitedTraffic: 1},
		{ID: 2, UUID: "b-uuid", SpeedLimit: 10, DeviceLimit: 1, PlanID: 5, UnlimitedTraffic: 0},
	})
	if got != want {
		t.Fatalf("md5 = %q, want %q", got, want)
	}

	empty := ComputeUsersMD5(nil)
	if empty == "" {
		t.Fatal("expected non-empty md5 for empty user list")
	}
}
