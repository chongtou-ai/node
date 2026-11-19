package xhttpdown

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/quic-go"
	qtls "github.com/metacubex/sing-quic"
	"github.com/metacubex/sing-quic/hysteria2"
	M "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"

	"github.com/cedar2025/xboard-node/internal/model"
)

func TestHysteria2DeliversSessionAndBytes(t *testing.T) {
	const (
		session  = "22222222-2222-4222-8222-222222222222"
		password = "test-password"
	)
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = packet.Close()
	port := packet.LocalAddr().(*net.UDPAddr).Port

	prev := serveDownlink
	serveDownlink = func(ctx context.Context, id string, conn net.Conn) error {
		if id != session {
			t.Errorf("session id = %q", id)
			_ = conn.Close()
			return io.ErrClosedPipe
		}
		if _, err := conn.Write([]byte("pong")); err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, conn)
		return nil
	}
	t.Cleanup(func() { serveDownlink = prev })

	srv, err := Listen(&model.XHTTPDownlink{
		Enabled:  true,
		ListenIP: "127.0.0.1",
		Port:     port,
		Password: password,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, client, err := dialDownlink(ctx, "127.0.0.1", port, password, session)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseWithError(nil)
	defer conn.Close()

	buf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("payload = %q", buf)
	}
}

func dialDownlink(ctx context.Context, host string, port int, password, session string) (net.Conn, *hysteria2.Client, error) {
	client, err := hysteria2.NewClient(hysteria2.ClientOptions{
		Context:       ctx,
		Logger:        nlogAdapter{},
		ServerAddress: M.ParseSocksaddrHostPort(host, uint16(port)),
		Password:      password,
		UDPDisabled:   true,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"h3"},
			ServerName:         "xhttp-downlink",
		},
		QuicDialer: qtls.QuicDialerFunc(dialQUIC),
	})
	if err != nil {
		return nil, nil, err
	}
	conn, err := client.DialConn(ctx, M.ParseSocksaddrHostPort(session, 443))
	if err != nil {
		_ = client.CloseWithError(err)
		return nil, nil, err
	}
	// The TCP request frame is sent on the first write. A downlink has no
	// client payload, so flush an empty request before reading.
	if _, err = conn.Write(nil); err != nil {
		_ = conn.Close()
		_ = client.CloseWithError(err)
		return nil, nil, err
	}
	return conn, client, nil
}

func dialQUIC(ctx context.Context, addr string, _ qtls.PacketDialer, tlsCfg *tls.Config, cfg *quic.Config, early bool) (net.PacketConn, *quic.Conn, error) {
	remote, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, nil, err
	}
	pc, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, nil, err
	}
	var conn *quic.Conn
	if early {
		conn, err = quic.DialEarly(ctx, pc, remote, tlsCfg, cfg)
	} else {
		conn, err = quic.Dial(ctx, pc, remote, tlsCfg, cfg)
	}
	if err != nil {
		_ = pc.Close()
		return nil, nil, err
	}
	return pc, conn, nil
}
