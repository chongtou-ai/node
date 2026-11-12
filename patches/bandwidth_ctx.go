package dispatcher

import (
	"context"

	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/bandwidth"
)

func bandwidthManagerFromContext(ctx context.Context) bandwidth.Manager {
	v := core.FromContext(ctx)
	if v == nil {
		return nil
	}
	feat := v.GetFeature(bandwidth.ManagerType())
	if feat == nil {
		return nil
	}
	bm, _ := feat.(bandwidth.Manager)
	return bm
}

// UserHasBandwidthLimit reports whether the inbound session user has an active
// per-user bandwidth limiter configured via the bandwidth feature.
func UserHasBandwidthLimit(ctx context.Context) bool {
	bm := bandwidthManagerFromContext(ctx)
	if bm == nil {
		return false
	}
	inbound := session.InboundFromContext(ctx)
	if inbound == nil || inbound.User == nil || inbound.User.Email == "" {
		return false
	}
	return bm.GetUserLimiter(inbound.User.Email) != nil
}

// DisableSpliceForBandwidthLimit forces copy-based relay so rate limiting hooks
// remain on the data path. XTLS Vision and kernel splice bypass buf.Writer.
func DisableSpliceForBandwidthLimit(ctx context.Context) {
	if inbound := session.InboundFromContext(ctx); inbound != nil {
		inbound.CanSpliceCopy = 3
	}
	for _, ob := range session.OutboundsFromContext(ctx) {
		if ob != nil {
			ob.CanSpliceCopy = 3
		}
	}
}
