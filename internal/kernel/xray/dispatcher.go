package xray

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	_ "unsafe"

	xrayDispatcher "github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/transport"

	"github.com/cedar2025/xboard-node/internal/nlog"
)

// Access xray's internal config creator registry so we can replace the
// default dispatcher factory with ours. This runs AFTER xray's init()
// functions because our package imports xray (dependency order guarantee).
//
//go:linkname typeCreatorRegistry github.com/xtls/xray-core/common.typeCreatorRegistry
var typeCreatorRegistry map[reflect.Type]common.ConfigCreator

var origDispatcherFactory common.ConfigCreator

// globalLimitDispatcher is set when the factory creates a LimitDispatcher.
// The Xray kernel reads it to configure limits and get connections.
var globalLimitDispatcher atomic.Pointer[LimitDispatcher]

func init() {
	configType := reflect.TypeOf((*xrayDispatcher.Config)(nil))
	origDispatcherFactory = typeCreatorRegistry[configType]
	typeCreatorRegistry[configType] = limitDispatcherFactory
}

func limitDispatcherFactory(ctx context.Context, config interface{}) (interface{}, error) {
	orig, err := origDispatcherFactory(ctx, config)
	if err != nil {
		return nil, err
	}
	inner, ok := orig.(routing.Dispatcher)
	if !ok {
		return orig, nil
	}
	ld := &LimitDispatcher{
		inner:         orig,
		innerDisp:     inner,
		limitedIPs:    make(map[string]map[string]int),
		activeWriters: make(map[string][]*closeTrackingWriter),
	}
	globalLimitDispatcher.Store(ld)
	nlog.Core().Debug("xray: limit dispatcher installed")
	return ld, nil
}

// LimitDispatcher wraps xray's DefaultDispatcher to enforce per-user
// admission checks before a request is dispatched into xray-core.
//
// It intentionally does NOT mutate transport.Link.Reader/Writer. Xray's
// mux/XUDP close path requires the original concrete *pipe.Reader to remain
// intact, so the dispatcher is limited to gate-keeping and safe connection
// lifecycle bookkeeping.
type LimitDispatcher struct {
	inner     interface{}        // original DefaultDispatcher (Feature + Dispatcher)
	innerDisp routing.Dispatcher // same object, typed as Dispatcher

	// limitedUsers: users with device limit > 0, protected by mu.
	// First-come-first-served: new IPs are rejected once the slot count is full.
	mu           sync.RWMutex
	limitedIPs   map[string]map[string]int // email → sourceIP → refcount
	deviceLimits map[string]int            // email → max devices
	emailToUID   map[string]int            // email → panel user ID

	// unlimitedIPs: users without device limit — sync.Map for lock-free access.
	// Each entry is *ipCounter{ips sync.Map}.
	unlimitedIPs sync.Map // email → *ipCounter

	activeMu      sync.Mutex
	activeWriters map[string][]*closeTrackingWriter // email → open link writers

	connCount atomic.Int64 // total active connections tracked by dispatcher
}

// ipCounter tracks IPs for unlimited users without any lock.
type ipCounter struct {
	ips sync.Map // sourceIP → *atomic.Int64 (refcount)
}

// aliveIPs returns a snapshot of distinct IPs.
func (ic *ipCounter) aliveIPs() map[string]bool {
	result := make(map[string]bool)
	ic.ips.Range(func(key, _ interface{}) bool {
		if rv, ok := ic.ips.Load(key); ok && rv.(*atomic.Int64).Load() > 0 {
			result[key.(string)] = true
		}
		return true
	})
	return result
}

// ─── routing.Dispatcher ──────────────────────────────────────────────────────

func (d *LimitDispatcher) Dispatch(ctx context.Context, dest net.Destination) (*transport.Link, error) {
	sess, err := d.identifyAndCheck(ctx, dest)
	if err != nil {
		return nil, err
	}

	link, err := d.innerDisp.Dispatch(ctx, dest)
	if err != nil {
		if sess.email != "" && sess.isTCP {
			d.delConn(sess.email, sess.sourceIP)
		}
		return nil, err
	}

	if sess.email != "" {
		d.trackLink(link, sess)
	}
	return link, nil
}

func (d *LimitDispatcher) DispatchLink(ctx context.Context, dest net.Destination, link *transport.Link) error {
	sess, err := d.identifyAndCheck(ctx, dest)
	if err != nil {
		return err
	}

	if sess.email != "" {
		d.trackLink(link, sess)
	}
	return d.innerDisp.DispatchLink(ctx, dest, link)
}

// sessionRef is the per-dispatch user identity used for device-limit
// bookkeeping and forced disconnects.
type sessionRef struct {
	email    string
	sourceIP string
	isTCP    bool
	conn     net.Conn // client-side connection, may be nil (e.g. UDP inbounds)
}

// identifyAndCheck extracts user identity from the session context and enforces
// device limits. Returns a non-nil error only when the connection should be
// rejected.
func (d *LimitDispatcher) identifyAndCheck(ctx context.Context, dest net.Destination) (sessionRef, error) {
	si := session.InboundFromContext(ctx)
	if si == nil || si.User == nil || len(si.User.Email) == 0 {
		return sessionRef{}, nil
	}
	sess := sessionRef{
		email:    si.User.Email,
		sourceIP: si.Source.Address.IP().String(),
		isTCP:    dest.Network == net.Network_TCP,
		conn:     si.Conn,
	}

	if d.checkDeviceLimit(sess.email, sess.sourceIP, sess.isTCP) {
		nlog.Core().Debug("xray: device limit exceeded", "email", sess.email, "ip", sess.sourceIP)
		return sessionRef{}, errors.New("device limit exceeded for " + sess.email)
	}
	return sess, nil
}

// trackLink records connection lifecycle without mutating xray-core owned
// transport primitives. This keeps mux/XUDP compatible while still allowing
// the dispatcher to release device-limit state when the link closes.
func (d *LimitDispatcher) trackLink(link *transport.Link, sess sessionRef) {
	d.connCount.Add(1)

	cw := &closeTrackingWriter{Writer: link.Writer, conn: sess.conn}
	cw.onClose = func() {
		d.unregisterWriter(sess.email, cw)
		if sess.isTCP {
			d.delConn(sess.email, sess.sourceIP)
		}
		d.connCount.Add(-1)
	}
	d.registerWriter(sess.email, cw)
	link.Writer = cw
}

func (d *LimitDispatcher) registerWriter(email string, cw *closeTrackingWriter) {
	if email == "" || cw == nil {
		return
	}
	d.activeMu.Lock()
	d.activeWriters[email] = append(d.activeWriters[email], cw)
	d.activeMu.Unlock()
}

func (d *LimitDispatcher) unregisterWriter(email string, cw *closeTrackingWriter) {
	if email == "" || cw == nil {
		return
	}
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	list := d.activeWriters[email]
	for i, w := range list {
		if w == cw {
			d.activeWriters[email] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(d.activeWriters[email]) == 0 {
		delete(d.activeWriters, email)
	}
}

// CloseUserConnections interrupts all active links for the given user email.
//
// Closing the tracked writer alone is not enough: kernel splice and XTLS Vision
// direct copy move bytes without ever touching the writer chain, so an expired
// user could keep streaming until the peer hung up. The client connection is
// therefore dropped as well, which terminates those paths immediately.
func (d *LimitDispatcher) CloseUserConnections(email string) int {
	if email == "" {
		return 0
	}
	d.activeMu.Lock()
	writers := d.activeWriters[email]
	delete(d.activeWriters, email)
	d.activeMu.Unlock()

	closed := 0
	droppedConns := make(map[net.Conn]struct{}, len(writers))
	for _, w := range writers {
		if w == nil {
			continue
		}
		if err := w.Close(); err == nil {
			closed++
		}
		// One client conn can back several links (mux/XUDP); close it once.
		if w.conn != nil {
			if _, done := droppedConns[w.conn]; !done {
				droppedConns[w.conn] = struct{}{}
				w.conn.Close()
			}
		}
	}
	if closed > 0 || len(droppedConns) > 0 {
		nlog.Core().Info("xray: closed user connections",
			"email", email, "links", closed, "conns", len(droppedConns))
	}
	return closed
}

// ─── features.Feature (delegated) ───────────────────────────────────────────

func (d *LimitDispatcher) Type() interface{} { return routing.DispatcherType() }

func (d *LimitDispatcher) Start() error {
	if s, ok := d.inner.(interface{ Start() error }); ok {
		return s.Start()
	}
	return nil
}

func (d *LimitDispatcher) Close() error {
	if c, ok := d.inner.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// ─── Limit management (called by Xray kernel) ──────────────────────────────

func (d *LimitDispatcher) UpdateLimits(emailToUID map[string]int, deviceLimits, _ map[string]int) {
	d.mu.Lock()
	d.emailToUID = emailToUID
	d.deviceLimits = deviceLimits
	d.mu.Unlock()

}

func (d *LimitDispatcher) ResetConns() {
	d.mu.Lock()
	d.limitedIPs = make(map[string]map[string]int)
	d.mu.Unlock()

	d.activeMu.Lock()
	d.activeWriters = make(map[string][]*closeTrackingWriter)
	d.activeMu.Unlock()

	// Clear unlimited IPs
	d.unlimitedIPs.Range(func(key, _ interface{}) bool {
		d.unlimitedIPs.Delete(key)
		return true
	})

	d.connCount.Store(0)
}

// GetConnectionState returns dispatcher-tracked alive IPs and connection count.
// Traffic bytes are intentionally left to xray's built-in stats pipeline.
func (d *LimitDispatcher) GetConnectionState() (aliveIPs map[int]map[string]bool, connCount int) {
	d.mu.RLock()
	emailToUID := d.emailToUID
	limitedIPs := d.limitedIPs
	d.mu.RUnlock()

	aliveIPs = make(map[int]map[string]bool)

	// Collect IPs from limited users (under RLock snapshot).
	for email, ipsMap := range limitedIPs {
		uid := emailToUID[email]
		if uid == 0 {
			continue
		}
		ipSet := make(map[string]bool, len(ipsMap))
		for ip := range ipsMap {
			ipSet[ip] = true
		}
		if len(ipSet) > 0 {
			aliveIPs[uid] = ipSet
		}
	}

	// Collect IPs from unlimited users (lock-free).
	d.unlimitedIPs.Range(func(key, value interface{}) bool {
		email := key.(string)
		uid := emailToUID[email]
		if uid == 0 {
			return true
		}
		ic := value.(*ipCounter)
		if ips := ic.aliveIPs(); len(ips) > 0 {
			// Merge with limited IPs if any
			if existing, ok := aliveIPs[uid]; ok {
				for ip := range ips {
					existing[ip] = true
				}
			} else {
				aliveIPs[uid] = ips
			}
		}
		return true
	})

	connCount = int(d.connCount.Load())
	return
}

// ─── Internal helpers ───────────────────────────────────────────────────────

// checkDeviceLimit enforces per-user device limits.
// Fast path: unlimited users use lock-free sync.Map.
// Slow path: limited users, first-come-first-served — a new IP is rejected
// once the user already has `limit` distinct IPs. Existing IPs stay allowed.
func (d *LimitDispatcher) checkDeviceLimit(email, sourceIP string, isTCP bool) bool {
	d.mu.RLock()
	limit, hasLimit := d.deviceLimits[email]
	d.mu.RUnlock()

	// Fast path: no device limit — use lock-free sync.Map.
	if !hasLimit || limit <= 0 {
		if isTCP {
			v, _ := d.unlimitedIPs.LoadOrStore(email, &ipCounter{})
			ic := v.(*ipCounter)

			// Increment IP refcount atomically.
			rv, _ := ic.ips.LoadOrStore(sourceIP, &atomic.Int64{})
			rv.(*atomic.Int64).Add(1)
		}
		return false
	}

	// Known IP: increment under write lock (map may be replaced by ResetConns).
	d.mu.RLock()
	ips := d.limitedIPs[email]
	known := ips != nil && ips[sourceIP] > 0
	d.mu.RUnlock()
	if known {
		if isTCP {
			d.mu.Lock()
			if d.limitedIPs[email] != nil {
				d.limitedIPs[email][sourceIP]++
			}
			d.mu.Unlock()
		}
		return false
	}

	// New IP: admit or reject under write lock so two IPs cannot race past limit.
	d.mu.Lock()
	defer d.mu.Unlock()

	ips = d.limitedIPs[email]
	if ips == nil {
		ips = make(map[string]int)
		d.limitedIPs[email] = ips
	}

	if ips[sourceIP] > 0 {
		if isTCP {
			ips[sourceIP]++
		}
		return false
	}

	if len(ips) < limit {
		if isTCP {
			ips[sourceIP]++
		}
		return false
	}

	return true
}

// delConn decrements the IP refcount when a connection closes.
func (d *LimitDispatcher) delConn(email, sourceIP string) {
	// Check if this is an unlimited user first (lock-free).
	if v, ok := d.unlimitedIPs.Load(email); ok {
		ic := v.(*ipCounter)
		if rv, ok := ic.ips.Load(sourceIP); ok {
			counter := rv.(*atomic.Int64)
			if counter.Add(-1) <= 0 {
				ic.ips.Delete(sourceIP)
			}
		}
		return
	}

	// Limited user — use write lock.
	d.mu.Lock()
	defer d.mu.Unlock()
	if ips, ok := d.limitedIPs[email]; ok {
		ips[sourceIP]--
		if ips[sourceIP] <= 0 {
			delete(ips, sourceIP)
		}
		if len(ips) == 0 {
			delete(d.limitedIPs, email)
		}
	}
}

type closeTrackingWriter struct {
	buf.Writer
	conn    net.Conn
	onClose func()
	closed  atomic.Bool
}

func (w *closeTrackingWriter) Close() error {
	if w.closed.CompareAndSwap(false, true) {
		w.onClose()
	}
	return common.Close(w.Writer)
}

func (w *closeTrackingWriter) Interrupt() {
	if w.closed.CompareAndSwap(false, true) {
		w.onClose()
	}
	common.Interrupt(w.Writer)
}
