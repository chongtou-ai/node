package splithttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/testing/servers/tcp"
	"github.com/xtls/xray-core/transport/internet"
	. "github.com/xtls/xray-core/transport/internet/splithttp"
	"github.com/xtls/xray-core/transport/internet/stat"
)

func TestPacketUpReturnsJSONAck(t *testing.T) {
	listenPort := tcp.PickPort()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "splithttp",
		ProtocolSettings: &Config{
			Path: "/xhttp",
			Mode: "packet-up",
		},
	}
	listen, err := ListenXH(context.Background(), net.LocalHostIP, listenPort, streamSettings, func(conn stat.Connection) {
		go func() {
			_, _ = io.Copy(io.Discard, conn)
			_ = conn.Close()
		}()
	})
	common.Must(err)
	defer listen.Close()

	base := "http://" + net.LocalHostIP.String() + ":" + listenPort.String()
	session := "11111111-2222-3333-4444-555555555555"
	payload := []byte("hello-uplink")

	reqURL := base + "/xhttp/" + session + "/0?x_padding=" + strings.Repeat("X", 120)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(payload))
	common.Must(err)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	common.Must(err)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type=%q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	common.Must(err)

	var ack struct {
		Status  string      `json:"status"`
		Message string      `json:"message"`
		Data    string      `json:"data"`
		Error   interface{} `json:"error"`
	}
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if ack.Status != "success" {
		t.Fatalf("status=%q", ack.Status)
	}
	if ack.Message != "操作成功" {
		t.Fatalf("message=%q", ack.Message)
	}
	if ack.Data == "" {
		t.Fatal("expected non-empty data")
	}
	if ack.Error != nil {
		t.Fatalf("error=%v", ack.Error)
	}
	if !strings.Contains(string(body), `\u64cd\u4f5c\u6210\u529f`) {
		t.Fatalf("expected unicode-escaped message in raw body: %s", body)
	}
}
