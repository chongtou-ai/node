package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	bandwidthfeat "github.com/xtls/xray-core/features/bandwidth"
	featurepolicy "github.com/xtls/xray-core/features/policy"
	featurestats "github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport"
	"golang.org/x/time/rate"
)

type stubWriter struct{ calls int }

func (w *stubWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.calls++
	buf.ReleaseMulti(mb)
	return nil
}
func (w *stubWriter) Close() error { return nil }
func (w *stubWriter) Interrupt()   {}

type stubReader struct{}

func (stubReader) ReadMultiBuffer() (buf.MultiBuffer, error) { return nil, nil }
func (stubReader) Interrupt()                               {}

// sizedReader yields one buffer of the requested size per read.
type sizedReader struct {
	size  int32
	reads int
}

func (r *sizedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	r.reads++
	b := buf.New()
	b.Extend(r.size)
	return buf.MultiBuffer{b}, nil
}
func (r *sizedReader) Interrupt() {}

func TestApplyBandwidthLimitWrapsWriterOnly(t *testing.T) {
	bm := bandwidthfeat.New()
	bm.SetUserLimit("user@example.com", 1024)
	writer := &stubWriter{}
	reader := stubReader{}
	user := &protocol.MemoryUser{Email: "user@example.com"}
	wrapped := applyBandwidthLimit(context.Background(), writer, bm, user)
	if _, ok := wrapped.(*RateLimitWriter); !ok {
		t.Fatal("expected writer to be wrapped by RateLimitWriter")
	}
	if _, ok := any(reader).(stubReader); !ok {
		t.Fatal("reader type unexpectedly changed")
	}
}

func TestWrapLinkLimitsBothDirections(t *testing.T) {
	inst := new(core.Instance)
	bm := bandwidthfeat.New()
	bm.SetUserLimit("user@example.com", 1024)
	if err := inst.AddFeature(bm); err != nil {
		t.Fatalf("AddFeature(bandwidth) error = %v", err)
	}
	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx = session.ContextWithInbound(ctx, &session.Inbound{
		User:   &protocol.MemoryUser{Email: "user@example.com"},
		Source: net.TCPDestination(net.IPAddress([]byte{127, 0, 0, 1}), 1234),
	})
	link := &transport.Link{Reader: &buf.TimeoutWrapperReader{Reader: stubReader{}}, Writer: &stubWriter{}}
	_ = WrapLink(ctx, featurepolicy.DefaultManager{}, featurestats.NoopManager{}, link)

	if _, ok := link.Writer.(*RateLimitWriter); !ok {
		t.Fatal("expected WrapLink to wrap writer with bandwidth limiter")
	}
	rlr, ok := link.Reader.(*RateLimitReader)
	if !ok {
		t.Fatalf("expected WrapLink to wrap reader for uplink limiting, got %T", link.Reader)
	}
	// DispatchLink asserts buf.TimeoutReader on the wrapped reader when sniffing.
	if _, ok := any(rlr).(buf.TimeoutReader); !ok {
		t.Fatal("RateLimitReader must satisfy buf.TimeoutReader")
	}
	if _, ok := rlr.Reader.(*buf.TimeoutWrapperReader); !ok {
		t.Fatal("expected TimeoutWrapperReader to stay beneath the rate limiter")
	}
}

func TestWrapLinkLeavesUnlimitedUserUntouched(t *testing.T) {
	inst := new(core.Instance)
	bm := bandwidthfeat.New()
	if err := inst.AddFeature(bm); err != nil {
		t.Fatalf("AddFeature(bandwidth) error = %v", err)
	}
	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx = session.ContextWithInbound(ctx, &session.Inbound{
		User:          &protocol.MemoryUser{Email: "nolimit@example.com"},
		Source:        net.TCPDestination(net.IPAddress([]byte{127, 0, 0, 1}), 1234),
		CanSpliceCopy: 2,
	})
	link := &transport.Link{Reader: &buf.TimeoutWrapperReader{Reader: stubReader{}}, Writer: &stubWriter{}}
	_ = WrapLink(ctx, featurepolicy.DefaultManager{}, featurestats.NoopManager{}, link)

	rlw, ok := link.Writer.(*RateLimitWriter)
	if !ok {
		t.Fatalf("expected dynamic rate limit wrapper on writer, got %T", link.Writer)
	}
	if _, ok := link.Reader.(*RateLimitReader); !ok {
		t.Fatalf("expected dynamic rate limit wrapper on reader, got %T", link.Reader)
	}
	b := buf.New()
	b.Extend(1)
	if err := rlw.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatalf("unlimited write failed: %v", err)
	}
	if got := session.InboundFromContext(ctx).CanSpliceCopy; got != 2 {
		t.Fatalf("splice must stay enabled while unlimited, got CanSpliceCopy=%d", got)
	}
}

func TestApplyBandwidthLimitDisablesSplice(t *testing.T) {
	bm := bandwidthfeat.New()
	bm.SetUserLimit("user@example.com", 1024)
	user := &protocol.MemoryUser{Email: "user@example.com"}
	ctx := session.ContextWithInbound(context.Background(), &session.Inbound{
		User:          user,
		CanSpliceCopy: 2,
	})
	ctx = session.ContextWithOutbounds(ctx, []*session.Outbound{{CanSpliceCopy: 1}})
	wrapped := applyBandwidthLimit(ctx, &stubWriter{}, bm, user)
	b := buf.New()
	b.Extend(1)
	if err := wrapped.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatalf("limited write failed: %v", err)
	}

	if got := session.InboundFromContext(ctx).CanSpliceCopy; got != 3 {
		t.Fatalf("expected inbound CanSpliceCopy=3 when limited, got %d", got)
	}
	for _, ob := range session.OutboundsFromContext(ctx) {
		if ob.CanSpliceCopy != 3 {
			t.Fatalf("expected outbound CanSpliceCopy=3 when limited, got %d", ob.CanSpliceCopy)
		}
	}
}

func TestRateLimitWriterAppliesLimiterMidConnection(t *testing.T) {
	bm := bandwidthfeat.New()
	email := "user@example.com"
	w := &RateLimitWriter{
		Writer:  &stubWriter{},
		Manager: bm,
		Email:   email,
		Context: context.Background(),
	}
	b := buf.New()
	b.Extend(4096)
	if err := w.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatalf("unlimited write failed: %v", err)
	}

	bm.SetUserLimit(email, 4096)
	start := time.Now()
	b = buf.New()
	b.Extend(8192)
	if err := w.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatalf("limited write failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("expected mid-connection limiter to throttle, took only %v", elapsed)
	}
}

// A buffer larger than the bucket burst must still pass. rate.Limiter.WaitN
// rejects n > burst outright, which used to drop the connection.
func TestWaitBytesChunksAboveBurst(t *testing.T) {
	lim := rate.NewLimiter(rate.Limit(1<<20), 4096)
	if err := waitBytes(context.Background(), lim, 64*1024); err != nil {
		t.Fatalf("waitBytes above burst should chunk, got error: %v", err)
	}
}

func TestRateLimitWriterAcceptsBufferAboveBurst(t *testing.T) {
	w := &RateLimitWriter{
		Writer:  &stubWriter{},
		Limiter: rate.NewLimiter(rate.Limit(1<<20), 4096),
		Context: context.Background(),
	}
	b := buf.New()
	b.Extend(8192)
	if err := w.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatalf("write above burst should succeed, got %v", err)
	}
}

func TestRateLimitWriterRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &RateLimitWriter{
		Writer:  &stubWriter{},
		Limiter: rate.NewLimiter(1, 1),
		Context: ctx,
	}
	b := buf.New()
	b.Extend(2)
	if err := w.WriteMultiBuffer(buf.MultiBuffer{b}); err == nil {
		t.Fatal("expected canceled context to abort wait")
	}
}

func TestRateLimitReaderThrottlesUplink(t *testing.T) {
	const chunk = 8192
	// 16 KiB/s with a 4 KiB burst: reading 3 x 8 KiB must take measurable time.
	r := &RateLimitReader{
		Reader:  &sizedReader{size: chunk},
		Limiter: rate.NewLimiter(rate.Limit(16*1024), 4096),
		Context: context.Background(),
	}
	start := time.Now()
	for i := 0; i < 3; i++ {
		mb, err := r.ReadMultiBuffer()
		if err != nil {
			t.Fatalf("read %d failed: %v", i, err)
		}
		if mb.Len() != chunk {
			t.Fatalf("expected %d bytes, got %d", chunk, mb.Len())
		}
		buf.ReleaseMulti(mb)
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("uplink was not throttled, 24 KiB at 16 KiB/s took only %v", elapsed)
	}
}

func TestRateLimitReaderRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &RateLimitReader{
		Reader:  &sizedReader{size: 4096},
		Limiter: rate.NewLimiter(1, 1),
		Context: ctx,
	}
	if _, err := r.ReadMultiBuffer(); err == nil {
		t.Fatal("expected canceled context to abort wait")
	}
}
