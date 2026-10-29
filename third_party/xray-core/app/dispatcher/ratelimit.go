package dispatcher

import (
	"context"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/features/bandwidth"
	"golang.org/x/time/rate"
)

// waitBytes consumes n tokens, split into chunks no larger than the bucket
// burst. rate.Limiter.WaitN rejects any request above the burst outright, which
// would otherwise surface as a dropped connection on large buffers.
func waitBytes(ctx context.Context, limiter *rate.Limiter, n int) error {
	if limiter == nil || n <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	burst := limiter.Burst()
	if burst <= 0 {
		return nil
	}
	for n > 0 {
		chunk := n
		if chunk > burst {
			chunk = burst
		}
		if err := limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

type RateLimitWriter struct {
	Writer  buf.Writer
	Context context.Context

	// Limiter is used by unit tests; production uses Manager+Email lookup so
	// mid-connection speed changes apply without reconnecting.
	Limiter *rate.Limiter
	Manager bandwidth.Manager
	Email   string
}

func (w *RateLimitWriter) activeLimiter() *rate.Limiter {
	if w.Limiter != nil {
		return w.Limiter
	}
	if w.Manager != nil && w.Email != "" {
		return w.Manager.GetUserLimiter(w.Email)
	}
	return nil
}

func (w *RateLimitWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if lim := w.activeLimiter(); lim != nil {
		DisableSpliceForBandwidthLimit(w.Context)
		if err := waitBytes(w.Context, lim, int(mb.Len())); err != nil {
			buf.ReleaseMulti(mb)
			return err
		}
	}
	return w.Writer.WriteMultiBuffer(mb)
}

func (w *RateLimitWriter) Close() error { return common.Close(w.Writer) }
func (w *RateLimitWriter) Interrupt()   { common.Interrupt(w.Writer) }

// RateLimitReader throttles the uplink direction. Writer-side limiting alone
// only covers downlink, and inbounds that hand a full transport.Link to
// DispatchLink (VLESS, Hysteria, mux, SOCKS, HTTP) never get their uplink
// wrapped otherwise.
//
// Tokens are charged against the bytes actually read, so a large read buffer
// that returns few bytes is not over-billed.
type RateLimitReader struct {
	Reader  buf.Reader
	Context context.Context

	Limiter *rate.Limiter
	Manager bandwidth.Manager
	Email   string
}

func (r *RateLimitReader) activeLimiter() *rate.Limiter {
	if r.Limiter != nil {
		return r.Limiter
	}
	if r.Manager != nil && r.Email != "" {
		return r.Manager.GetUserLimiter(r.Email)
	}
	return nil
}

func (r *RateLimitReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if throttleErr := r.throttle(mb); throttleErr != nil {
		buf.ReleaseMulti(mb)
		return nil, throttleErr
	}
	return mb, err
}

func (r *RateLimitReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	tr, ok := r.Reader.(buf.TimeoutReader)
	if !ok {
		return r.ReadMultiBuffer()
	}
	mb, err := tr.ReadMultiBufferTimeout(timeout)
	if throttleErr := r.throttle(mb); throttleErr != nil {
		buf.ReleaseMulti(mb)
		return nil, throttleErr
	}
	return mb, err
}

func (r *RateLimitReader) throttle(mb buf.MultiBuffer) error {
	if mb.IsEmpty() {
		return nil
	}
	if lim := r.activeLimiter(); lim != nil {
		DisableSpliceForBandwidthLimit(r.Context)
		return waitBytes(r.Context, lim, int(mb.Len()))
	}
	return nil
}

func (r *RateLimitReader) Interrupt() { common.Interrupt(r.Reader) }
