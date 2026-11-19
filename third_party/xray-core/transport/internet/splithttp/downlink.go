package splithttp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/signal/done"
	"github.com/xtls/xray-core/transport/internet/stat"
)

// DownlinkPort is the TCP port the client puts in the Hysteria2 destination.
// The session is matched by host (the xhttp session id). This port is ignored.
const DownlinkPort = 443

// downlinkWait is how long a Hysteria2 stream waits for the xhttp POST that
// creates the session. The client can open the stream before the first POST.
var downlinkWait = 15 * time.Second

// downlinkSlot is the process-wide registration for one xhttp session.
// HTTP GET and Hysteria2 both claim it; the first claim owns the downlink.
type downlinkSlot struct {
	ready       chan struct{}
	readyClosed bool
	session     *httpSession
	handler     *requestHandler
	rawID       string
	claimed     bool
	dead        bool
}

var (
	downlinkMu    sync.Mutex
	downlinkSlots = map[string]*downlinkSlot{}
)

func normalizeDownlinkID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// tcpAddrs returns addresses xray's inbound worker can classify.
// Hysteria2 streams report a custom address type, which that worker rejects.
func tcpAddrs(conn net.Conn) (remote, local net.Addr) {
	remote = asTCPAddr(conn.RemoteAddr())
	local = asTCPAddr(conn.LocalAddr())
	if remote == nil {
		remote = &net.TCPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 1}
	}
	if local == nil {
		local = &net.TCPAddr{IP: net.IPv4(198, 18, 0, 2), Port: 1}
	}
	return remote, local
}

func asTCPAddr(addr net.Addr) net.Addr {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a
	case *net.UDPAddr:
		return &net.TCPAddr{IP: a.IP, Port: a.Port}
	default:
		return nil
	}
}

func downlinkSlotFor(id string) *downlinkSlot {
	id = normalizeDownlinkID(id)
	downlinkMu.Lock()
	defer downlinkMu.Unlock()
	slot, ok := downlinkSlots[id]
	if !ok {
		slot = &downlinkSlot{ready: make(chan struct{})}
		downlinkSlots[id] = slot
	}
	return slot
}

func publishDownlink(sessionID string, session *httpSession, handler *requestHandler) {
	id := normalizeDownlinkID(sessionID)
	if id == "" || session == nil || handler == nil {
		return
	}
	slot := downlinkSlotFor(id)
	downlinkMu.Lock()
	defer downlinkMu.Unlock()
	if slot.dead || slot.claimed {
		return
	}
	slot.session = session
	slot.handler = handler
	slot.rawID = sessionID
	if !slot.readyClosed {
		slot.readyClosed = true
		close(slot.ready)
	}
}

func revokeDownlink(sessionID string) {
	id := normalizeDownlinkID(sessionID)
	downlinkMu.Lock()
	defer downlinkMu.Unlock()
	slot := downlinkSlots[id]
	if slot == nil {
		return
	}
	delete(downlinkSlots, id)
	if slot.claimed {
		return
	}
	slot.dead = true
	if !slot.readyClosed {
		slot.readyClosed = true
		close(slot.ready)
	}
}

func tryClaimDownlink(id string) (*downlinkSlot, bool) {
	id = normalizeDownlinkID(id)
	downlinkMu.Lock()
	defer downlinkMu.Unlock()
	slot := downlinkSlots[id]
	if slot == nil || slot.dead || slot.claimed || slot.session == nil || slot.handler == nil {
		return nil, false
	}
	slot.claimed = true
	return slot, true
}

// ClaimDownlink reserves the HTTP GET downlink for sessionID.
// Hysteria2 calls the same claim; the first caller wins.
func ClaimDownlink(id string) bool {
	_, ok := tryClaimDownlink(id)
	return ok
}

// ServeDownlink splices conn onto the xhttp session named by id.
// id is the Hysteria2 destination host. Uplink bytes stay on the HTTP POST
// queue; bytes written by the proxy are written to conn. Blocks until the
// spliced connection closes.
func ServeDownlink(ctx context.Context, id string, conn net.Conn) error {
	id = normalizeDownlinkID(id)
	if id == "" {
		_ = conn.Close()
		return fmt.Errorf("xhttp downlink: empty session id")
	}
	slot := downlinkSlotFor(id)
	timer := time.NewTimer(downlinkWait)
	defer timer.Stop()
	select {
	case <-slot.ready:
	case <-ctx.Done():
		_ = conn.Close()
		return ctx.Err()
	case <-timer.C:
		_ = conn.Close()
		return fmt.Errorf("xhttp downlink: session %s not found", id)
	}

	claimed, ok := tryClaimDownlink(id)
	if !ok {
		_ = conn.Close()
		return fmt.Errorf("xhttp downlink: session %s unavailable", id)
	}
	claimed.session.isFullyConnected.Close()

	handler := claimed.handler
	rawID := claimed.rawID
	finished := done.New()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			if handler != nil && rawID != "" {
				handler.sessions.Delete(rawID)
			}
			_ = claimed.session.uploadQueue.Close()
			revokeDownlink(id)
			_ = finished.Close()
		})
	}

	if handler == nil || handler.ln == nil || handler.ln.addConn == nil {
		finish()
		_ = conn.Close()
		return fmt.Errorf("xhttp downlink: session %s has no listener", id)
	}

	errors.LogInfo(ctx, "xhttp hysteria2 downlink session ", rawID)
	remoteAddr, localAddr := tcpAddrs(conn)
	sc := &splitConn{
		writer:     conn,
		reader:     claimed.session.uploadQueue,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
		onClose:    finish,
	}
	handler.ln.addConn(stat.Connection(sc))

	select {
	case <-finished.Wait():
		return nil
	case <-ctx.Done():
		_ = sc.Close()
		<-finished.Wait()
		return ctx.Err()
	}
}
