package splithttp_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/testing/servers/tcp"
	"github.com/xtls/xray-core/transport/internet"
	. "github.com/xtls/xray-core/transport/internet/splithttp"
	"github.com/xtls/xray-core/transport/internet/stat"
)

func TestSpeedDownEndpoint(t *testing.T) {
	listenPort := tcp.PickPort()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "splithttp",
		ProtocolSettings: &Config{
			Path:          "/xhttp",
			SpeedDownPath: "/__down",
		},
	}
	listen, err := ListenXH(context.Background(), net.LocalHostIP, listenPort, streamSettings, func(conn stat.Connection) {
		go func() { _ = conn.Close() }()
	})
	common.Must(err)
	defer listen.Close()

	base := "http://" + net.LocalHostIP.String() + ":" + listenPort.String()

	resp, err := http.Get(base + "/__down?during=download&bytes=4096")
	common.Must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") != "4096" {
		t.Fatalf("Content-Length=%q", resp.Header.Get("Content-Length"))
	}
	body, err := io.ReadAll(resp.Body)
	common.Must(err)
	if len(body) != 4096 {
		t.Fatalf("body len=%d", len(body))
	}

	// xhttp path still 404 without session (not a valid xhttp client)
	resp2, err := http.Get(base + "/")
	common.Must(err)
	resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("root status=%d want 404", resp2.StatusCode)
	}
}

func TestSpeedDownCapsAt5GiB(t *testing.T) {
	listenPort := tcp.PickPort()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "splithttp",
		ProtocolSettings: &Config{
			Path:          "/xhttp",
			SpeedDownPath: "/__down",
		},
	}
	listen, err := ListenXH(context.Background(), net.LocalHostIP, listenPort, streamSettings, func(conn stat.Connection) {
		go func() { _ = conn.Close() }()
	})
	common.Must(err)
	defer listen.Close()

	base := "http://" + net.LocalHostIP.String() + ":" + listenPort.String()
	req, err := http.NewRequest(http.MethodHead, base+"/__down?bytes=999999999999", nil)
	common.Must(err)
	resp, err := http.DefaultClient.Do(req)
	common.Must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") != "5368709120" { // 5 GiB
		t.Fatalf("Content-Length=%q want 5368709120", resp.Header.Get("Content-Length"))
	}
}

func TestSpeedDownDisabled(t *testing.T) {
	listenPort := tcp.PickPort()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "splithttp",
		ProtocolSettings: &Config{
			Path:          "/xhttp",
			SpeedDownPath: "off",
		},
	}
	listen, err := ListenXH(context.Background(), net.LocalHostIP, listenPort, streamSettings, func(conn stat.Connection) {
		go func() { _ = conn.Close() }()
	})
	common.Must(err)
	defer listen.Close()

	base := "http://" + net.LocalHostIP.String() + ":" + listenPort.String()
	resp, err := http.Get(base + "/__down?bytes=1024")
	common.Must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("disabled __down status=%d want 404", resp.StatusCode)
	}
}
