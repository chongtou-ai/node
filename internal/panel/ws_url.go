package panel

import (
	"net"
	"net/url"
	"strings"

	"github.com/cedar2025/xboard-node/internal/nlog"
)

// resolvePanelWSURL fixes handshake ws_url values that drop a non-default
// panel port. Panels often generate ws://host/ws from APP_URL=http://host
// while the node reaches the panel at http://host:7001.
func resolvePanelWSURL(panelBaseURL, wsURL string) string {
	wsURL = strings.TrimSpace(wsURL)
	if wsURL == "" {
		return wsURL
	}
	panel, err := url.Parse(strings.TrimSpace(panelBaseURL))
	if err != nil || panel.Hostname() == "" {
		return wsURL
	}

	ws, err := url.Parse(wsURL)
	if err != nil {
		return wsURL
	}

	if ws.Scheme == "" || ws.Host == "" {
		resolved, err := panel.Parse(wsURL)
		if err != nil {
			return wsURL
		}
		switch resolved.Scheme {
		case "http":
			resolved.Scheme = "ws"
		case "https":
			resolved.Scheme = "wss"
		}
		return resolved.String()
	}

	if !strings.EqualFold(panel.Hostname(), ws.Hostname()) {
		return wsURL
	}
	if ws.Port() != "" || panel.Port() == "" {
		return wsURL
	}

	ws.Host = net.JoinHostPort(ws.Hostname(), panel.Port())
	fixed := ws.String()
	if fixed != wsURL {
		nlog.Core().Info("rewrote handshake ws_url to include panel port",
			"from", wsURL, "to", fixed, "panel", panelBaseURL)
	}
	return fixed
}
