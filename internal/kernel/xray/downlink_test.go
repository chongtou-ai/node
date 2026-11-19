package xray

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/quic-go"
	qtls "github.com/metacubex/sing-quic"
	"github.com/metacubex/sing-quic/hysteria2"
	meta "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vless/encoding"

	"github.com/metacubex/sing/common/logger"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
)

func TestXHTTPHysteria2DownlinkEcho(t *testing.T) {
	const (
		userID   = "11111111-1111-4111-8111-111111111111"
		session  = "22222222-2222-4222-8222-222222222222"
		password = "test-password"
	)
	echo := newEcho(t)
	xhttpPort := freePort(t, "tcp")
	downPort := freePort(t, "udp")

	core := New(config.KernelConfig{
		Type:     "xray",
		LogLevel: "debug",
		CustomRoute: []map[string]any{{
			"type":        "field",
			"ip":          []any{"127.0.0.1/32"},
			"outboundTag": "direct",
		}},
	})
	err := core.Start(&model.NodeSpec{
		Protocol:   "vless",
		ListenIP:   "127.0.0.1",
		ServerPort: xhttpPort,
		Network:    "xhttp",
		NetworkSettings: map[string]any{
			"path": "/",
			"mode": "packet-up",
		},
		XHTTPDownlink: &model.XHTTPDownlink{
			Enabled:  true,
			ListenIP: "127.0.0.1",
			Port:     downPort,
			Password: password,
		},
	}, []model.UserSpec{{ID: 1, UUID: userID}}, kernel.TLSCert{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(core.Stop)

	body, err := vlessTCP(userID, echo.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	padding := strings.Repeat("X", 200)
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+itoa(xhttpPort)+"/"+session+"/0?x_padding="+padding, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uplink POST status %d body %q", resp.StatusCode, respBody)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, client, err := dialHysteria(ctx, downPort, password, session)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseWithError(nil)
	defer conn.Close()

	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		t.Fatal(err)
	}
	if !bytes.Contains(buf[:n], []byte("pong")) {
		t.Fatalf("downlink = %q", buf[:n])
	}
}

func newEcho(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("pong"))
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	return ln
}

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := pc.LocalAddr().(*net.UDPAddr).Port
		_ = pc.Close()
		return port
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func vlessTCP(userID string, port int) ([]byte, error) {
	account, err := (&vless.Account{Id: userID}).AsAccount()
	if err != nil {
		return nil, err
	}
	if _, err := uuid.ParseString(userID); err != nil {
		return nil, err
	}
	var header bytes.Buffer
	err = encoding.EncodeRequestHeader(&header, &protocol.RequestHeader{
		Version: encoding.Version,
		User:    &protocol.MemoryUser{Account: account},
		Command: protocol.RequestCommandTCP,
		Address: xnet.IPAddress(net.ParseIP("127.0.0.1").To4()),
		Port:    xnet.Port(port),
	}, &encoding.Addons{})
	return header.Bytes(), err
}

func dialHysteria(ctx context.Context, port int, password, session string) (net.Conn, *hysteria2.Client, error) {
	client, err := hysteria2.NewClient(hysteria2.ClientOptions{
		Context:       ctx,
		Logger:        logger.NOP(),
		ServerAddress: meta.ParseSocksaddrHostPort("127.0.0.1", uint16(port)),
		Password:      password,
		UDPDisabled:   true,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"h3"},
			ServerName:         "xhttp-downlink",
		},
		QuicDialer: qtls.QuicDialerFunc(func(ctx context.Context, addr string, _ qtls.PacketDialer, tlsCfg *tls.Config, cfg *quic.Config, early bool) (net.PacketConn, *quic.Conn, error) {
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
		}),
	})
	if err != nil {
		return nil, nil, err
	}
	conn, err := client.DialConn(ctx, meta.ParseSocksaddrHostPort(session, 443))
	if err != nil {
		_ = client.CloseWithError(err)
		return nil, nil, err
	}
	if _, err = conn.Write(nil); err != nil {
		_ = conn.Close()
		_ = client.CloseWithError(err)
		return nil, nil, err
	}
	return conn, client, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
