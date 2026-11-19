package splithttp

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/transport/internet/stat"
)

func newDownlinkHandler(addConn func(stat.Connection)) *requestHandler {
	cfg := &Config{}
	return &requestHandler{
		config:    cfg,
		ln:        &Listener{addConn: addConn, config: cfg},
		sessionMu: &sync.Mutex{},
	}
}

func TestServeDownlinkSplicesUploadAndDownload(t *testing.T) {
	uplink := make(chan string, 1)
	h := newDownlinkHandler(func(c stat.Connection) {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Errorf("read uplink: %v", err)
			_ = c.Close()
			return
		}
		if _, err := c.Write([]byte("down")); err != nil {
			t.Errorf("write downlink: %v", err)
		}
		uplink <- string(buf)
		_ = c.Close()
	})

	session := h.upsertSession("AbC")
	if err := session.uploadQueue.Push(Packet{Seq: 0, Payload: []byte("ping")}); err != nil {
		t.Fatal(err)
	}

	client, server := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeDownlink(context.Background(), "abc", server)
	}()

	buf := make([]byte, 4)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read downlink: %v", err)
	}
	if string(buf) != "down" {
		t.Fatalf("downlink = %q", buf)
	}
	select {
	case got := <-uplink:
		if got != "ping" {
			t.Fatalf("uplink = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("uplink was not delivered")
	}
	_ = client.Close()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeDownlink did not return")
	}
}

func TestServeDownlinkRejectsSecondClaim(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := newDownlinkHandler(func(c stat.Connection) {
		close(entered)
		<-release
		_ = c.Close()
	})
	h.upsertSession("session")

	client, server := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeDownlink(context.Background(), "session", server)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first claim did not attach")
	}
	if ClaimDownlink("SESSION") {
		t.Fatal("second claim succeeded")
	}
	lateClient, lateServer := net.Pipe()
	defer lateClient.Close()
	if err := ServeDownlink(context.Background(), "session", lateServer); err == nil {
		t.Fatal("second ServeDownlink succeeded")
	}
	close(release)
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first ServeDownlink did not return")
	}
}

func TestServeDownlinkTimesOutWithoutSession(t *testing.T) {
	prev := downlinkWait
	downlinkWait = 30 * time.Millisecond
	t.Cleanup(func() { downlinkWait = prev })

	client, server := net.Pipe()
	defer client.Close()
	if err := ServeDownlink(context.Background(), "missing", server); err == nil {
		t.Fatal("expected timeout")
	}
}
