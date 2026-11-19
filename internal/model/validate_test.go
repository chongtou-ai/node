package model

import "testing"

func TestResolveKernelForProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		kernel   string
		want     string
	}{
		{"anytls", "xray", "singbox"},
		{"hysteria2", "xray", "singbox"},
		{"tuic", "xray", "singbox"},
		{"vless", "xray", "xray"},
		{"anytls", "singbox", "singbox"},
		{"vmess", "singbox", "singbox"},
	}
	for _, tc := range tests {
		if got := ResolveKernelForProtocol(tc.protocol, tc.kernel); got != tc.want {
			t.Errorf("ResolveKernelForProtocol(%q, %q)=%q, want %q", tc.protocol, tc.kernel, got, tc.want)
		}
	}
}

func TestValidateXHTTPDownlink(t *testing.T) {
	ok := &NodeSpec{
		Network:         "xhttp",
		ServerPort:      8001,
		NetworkSettings: map[string]any{"mode": "packet-up"},
		XHTTPDownlink:   &XHTTPDownlink{Enabled: true, ListenIP: "0.0.0.0", Port: 8443, Password: "pw"},
	}
	if err := ValidateXHTTPDownlink(ok, "xray"); err != nil {
		t.Fatal(err)
	}
	badMode := *ok
	badMode.NetworkSettings = map[string]any{"mode": "stream-up"}
	if err := ValidateXHTTPDownlink(&badMode, "xray"); err == nil {
		t.Fatal("stream-up was accepted")
	}
	if err := ValidateXHTTPDownlink(ok, "singbox"); err == nil {
		t.Fatal("sing-box was accepted")
	}
}

func TestResolveKernel(t *testing.T) {
	if got := ResolveKernel("", "anytls", "xray"); got != "singbox" {
		t.Fatalf("anytls+xray → %q, want singbox", got)
	}
	if got := ResolveKernel("xhttp", "vless", "singbox"); got != "xray" {
		t.Fatalf("xhttp+singbox → %q, want xray", got)
	}
	if got := ResolveKernel("ws", "vless", "xray"); got != "xray" {
		t.Fatalf("ws+xray → %q, want xray", got)
	}
}
