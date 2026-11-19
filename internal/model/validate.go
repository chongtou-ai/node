package model

import (
	"fmt"
	"net"
	"strings"

	"github.com/cedar2025/xboard-node/internal/config"
)

func ValidateNodeSpec(n *NodeSpec, kcfg config.KernelConfig) error {
	if n == nil {
		return nil
	}

	effectiveKernelType := strings.TrimSpace(kcfg.Type)
	if effectiveKernelType == "" {
		effectiveKernelType = strings.TrimSpace(n.KernelType)
	}
	kernelType, err := normalizeKernelType(effectiveKernelType)
	if err != nil {
		return fmt.Errorf("normalize kernel type: %w", err)
	}

	additionalOutboundSources, err := collectAdditionalOutboundTagSources(kcfg.CustomConfig, kcfg.CustomOutbound)
	if err != nil {
		return fmt.Errorf("collect additional outbound tags: %w", err)
	}
	if err := validateOutboundTagCollisions(n.CustomOutbounds, additionalOutboundSources); err != nil {
		return fmt.Errorf("validate outbound tags: %w", err)
	}
	additionalTags := additionalTagNames(additionalOutboundSources)
	availableTags := buildAvailableOutboundTags(n.CustomOutbounds, additionalTags)
	if err := ValidateCustomOutboundsForKernel(n.CustomOutbounds, kernelType, additionalTags); err != nil {
		return fmt.Errorf("validate custom outbounds: %w", err)
	}
	if err := ValidateCustomRouteRules(n.CustomRouteRules, kernelType, availableTags); err != nil {
		return fmt.Errorf("validate custom route rules: %w", err)
	}
	if err := validateTransportKernel(n.Network, kernelType); err != nil {
		return err
	}
	if err := ValidateXHTTPDownlink(n, kernelType); err != nil {
		return err
	}
	return nil
}

// ValidateXHTTPDownlink checks the Hysteria2 packet-up downlink.
// A disabled or absent config is valid.
func ValidateXHTTPDownlink(n *NodeSpec, kernelType string) error {
	if n == nil || n.XHTTPDownlink == nil || !n.XHTTPDownlink.Enabled {
		return nil
	}
	dl := n.XHTTPDownlink
	network := strings.ToLower(strings.TrimSpace(n.Network))
	if network != "xhttp" && network != "splithttp" {
		return fmt.Errorf("xhttp_downlink requires network xhttp")
	}
	if kernelType != "" && kernelType != "xray" {
		return fmt.Errorf("xhttp_downlink requires the xray kernel")
	}
	mode := ""
	if n.NetworkSettings != nil {
		mode, _ = n.NetworkSettings["mode"].(string)
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "packet-up", "auto":
	default:
		return fmt.Errorf("xhttp_downlink only supports packet-up mode, got %q", mode)
	}
	if dl.Port <= 0 || dl.Port > 65535 {
		return fmt.Errorf("xhttp_downlink.port must be between 1 and 65535")
	}
	if dl.Port == n.ServerPort {
		return fmt.Errorf("xhttp_downlink.port must be different from server_port")
	}
	if strings.TrimSpace(dl.Password) == "" {
		return fmt.Errorf("xhttp_downlink.password is required")
	}
	if ip := strings.TrimSpace(dl.ListenIP); ip != "" && net.ParseIP(ip) == nil {
		return fmt.Errorf("xhttp_downlink.listen_ip is not an IP address")
	}
	return nil
}

// singboxUnsupportedTransports lists transport types that sing-box does not support.
var singboxUnsupportedTransports = map[string]bool{
	"xhttp":     true,
	"splithttp": true,
}

func validateTransportKernel(network, kernelType string) error {
	net := strings.ToLower(strings.TrimSpace(network))
	if kernelType == "singbox" && singboxUnsupportedTransports[net] {
		return fmt.Errorf("transport %q is not supported by sing-box kernel; use xray kernel instead", net)
	}
	return nil
}

// ResolveKernelForTransport returns the kernel type required by the given
// transport. If the configured kernel cannot handle the transport, it returns
// the kernel that can. Otherwise it returns configuredKernel unchanged.
// This is used in machine mode to auto-switch kernel per node.
func ResolveKernelForTransport(network, configuredKernel string) string {
	net := strings.ToLower(strings.TrimSpace(network))
	if configuredKernel == "singbox" && singboxUnsupportedTransports[net] {
		return "xray"
	}
	return configuredKernel
}

// xrayUnsupportedProtocols are inbound protocols that only sing-box implements.
var xrayUnsupportedProtocols = map[string]bool{
	"anytls":    true,
	"hysteria2": true,
	"tuic":      true,
	"naive":     true,
	"mieru":     true,
	"socks":     true,
	"http":      true,
}

// ResolveKernelForProtocol returns the kernel type required by the given
// protocol. Xray-only installs are auto-switched to sing-box for protocols
// that xray-core cannot terminate (e.g. anytls).
func ResolveKernelForProtocol(protocol, configuredKernel string) string {
	proto := strings.ToLower(strings.TrimSpace(protocol))
	kernel := strings.ToLower(strings.TrimSpace(configuredKernel))
	if kernel == "" {
		kernel = "singbox"
	}
	if (kernel == "xray") && xrayUnsupportedProtocols[proto] {
		return "singbox"
	}
	return configuredKernel
}

// ResolveKernel picks the kernel that can run the given protocol + transport.
// Protocol requirements take precedence (e.g. anytls → singbox), then transport
// (e.g. xhttp → xray).
func ResolveKernel(network, protocol, configuredKernel string) string {
	k := ResolveKernelForProtocol(protocol, configuredKernel)
	return ResolveKernelForTransport(network, k)
}

func normalizeKernelType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "singbox", "sing-box":
		return "singbox", nil
	case "xray":
		return "xray", nil
	default:
		return "", fmt.Errorf("unsupported kernel type %q", value)
	}
}

func buildAvailableOutboundTags(structured []OutboundConfig, rawTags []string) map[string]struct{} {
	available := map[string]struct{}{
		"direct": {},
		"block":  {},
	}
	for _, outbound := range structured {
		tag := strings.ToLower(strings.TrimSpace(outbound.Tag))
		if tag != "" {
			available[tag] = struct{}{}
		}
	}
	for _, tag := range rawTags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			available[tag] = struct{}{}
		}
	}
	return available
}
