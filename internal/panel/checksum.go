package panel

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ComputeUsersMD5 returns a deterministic MD5 of the user list for sync checks.
// Users are sorted by uuid (a-z). Each user contributes:
// id&uuid&speed_limit&device_limit&plan_id&unlimited_traffic
func ComputeUsersMD5(users []User) string {
	if len(users) == 0 {
		sum := md5.Sum(nil)
		return hex.EncodeToString(sum[:])
	}

	sorted := make([]User, len(users))
	copy(sorted, users)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UUID < sorted[j].UUID
	})

	var parts []string
	for _, u := range sorted {
		parts = append(parts, fmt.Sprintf(
			"%d&%s&%d&%d&%d&%d",
			u.ID,
			u.UUID,
			u.SpeedLimit,
			u.DeviceLimit,
			u.PlanID,
			u.UnlimitedTraffic,
		))
	}

	sum := md5.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}
