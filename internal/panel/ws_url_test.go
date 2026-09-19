package panel

import (
	"encoding/json"
	"net"
	"net/url"
	"testing"
)

func TestResolvePanelWSURL(t *testing.T) {
	tests := []struct {
		name  string
		panel string
		wsURL string
		want  string
	}{
		{
			name:  "adds missing panel port",
			panel: "http://103.69.129.138:7001",
			wsURL: "ws://103.69.129.138/ws",
			want:  "ws://103.69.129.138:7001/ws",
		},
		{
			name:  "keeps explicit different ws port",
			panel: "http://103.69.129.138:7001",
			wsURL: "ws://103.69.129.138:8076/ws",
			want:  "ws://103.69.129.138:8076/ws",
		},
		{
			name:  "keeps already correct url",
			panel: "http://103.69.129.138:7001",
			wsURL: "ws://103.69.129.138:7001/ws",
			want:  "ws://103.69.129.138:7001/ws",
		},
		{
			name:  "does not rewrite different host",
			panel: "http://103.69.129.138:7001",
			wsURL: "ws://cdn.example.com/ws",
			want:  "ws://cdn.example.com/ws",
		},
		{
			name:  "resolves relative path",
			panel: "http://103.69.129.138:7001",
			wsURL: "/ws",
			want:  "ws://103.69.129.138:7001/ws",
		},
		{
			name:  "https panel uses wss for relative path",
			panel: "https://panel.example.com",
			wsURL: "/ws",
			want:  "wss://panel.example.com/ws",
		},
		{
			name:  "empty ws url stays empty",
			panel: "http://103.69.129.138:7001",
			wsURL: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePanelWSURL(tt.panel, tt.wsURL)
			if got != tt.want {
				t.Fatalf("resolvePanelWSURL(%q, %q) = %q, want %q", tt.panel, tt.wsURL, got, tt.want)
			}
		})
	}
}

func TestHandshake_RewritesMissingWSPort(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/server/handshake" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host == "" {
			host = "127.0.0.1"
		}
		_ = json.NewEncoder(w).Encode(HandshakeResponse{
			WebSocket: WSConfig{Enabled: true, WSURL: "ws://" + host + "/ws"},
			Settings:  Settings{PushInterval: 30, PullInterval: 30},
		})
	})
	defer ts.Close()

	hs, err := client.Handshake()
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}

	panel, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := "ws://" + panel.Host + "/ws"
	if hs.WebSocket.WSURL != want {
		t.Fatalf("ws_url = %q, want %q", hs.WebSocket.WSURL, want)
	}
}
