// Package xhttpdown serves an xhttp packet-up downlink on Hysteria2.
// The client dials with the xhttp session id as the destination host.
// Uplink stays on the HTTP POST listener; this listener only carries the
// bytes the proxy writes back.
package xhttpdown

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	http "github.com/metacubex/http"
	"github.com/metacubex/sing-quic/hysteria2"
	"github.com/metacubex/sing/common/logger"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
	"github.com/metacubex/tls"
	"github.com/xtls/xray-core/transport/internet/splithttp"

	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/cedar2025/xboard-node/internal/nlog"
)

// serveDownlink is the xray session splice. Tests replace it.
var serveDownlink = splithttp.ServeDownlink

// Server is a Hysteria2 listener bound to one xhttp downlink password.
type Server struct {
	service *hysteria2.Service[string]
	packet  net.PacketConn
	cancel  context.CancelFunc
}

// Listen starts the Hysteria2 downlink described by cfg.
// The client skips certificate checks, so the QUIC handshake uses an
// ephemeral self-signed certificate.
func Listen(cfg *model.XHTTPDownlink) (*Server, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("xhttp_downlink.port must be between 1 and 65535")
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("xhttp_downlink.password is required")
	}
	listenIP := cfg.ListenIP
	if listenIP == "" {
		listenIP = "0.0.0.0"
	}

	cert, err := selfSigned()
	if err != nil {
		return nil, fmt.Errorf("xhttp_downlink certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	service, err := hysteria2.NewService[string](hysteria2.ServiceOptions{
		Context:               ctx,
		Logger:                nlogAdapter{},
		TLSConfig:             tlsConfig,
		IgnoreClientBandwidth: true,
		UDPDisabled:           true,
		Handler:               &downlinkHandler{ctx: ctx},
		MasqueradeHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}),
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("xhttp_downlink hysteria2: %w", err)
	}
	service.UpdateUsers([]string{"xhttp"}, []string{cfg.Password})

	packet, err := net.ListenPacket("udp", net.JoinHostPort(listenIP, fmt.Sprintf("%d", cfg.Port)))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("xhttp_downlink listen %s:%d: %w", listenIP, cfg.Port, err)
	}
	if err := service.Start(packet); err != nil {
		_ = packet.Close()
		cancel()
		return nil, fmt.Errorf("xhttp_downlink start: %w", err)
	}
	nlog.Core().Info("xhttp hysteria2 downlink listening", "addr", packet.LocalAddr().String())
	return &Server{service: service, packet: packet, cancel: cancel}, nil
}

// Close stops the Hysteria2 listener and the streams spliced through it.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	err := s.service.Close()
	if s.packet != nil {
		if cerr := s.packet.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

type downlinkHandler struct {
	ctx context.Context
}

func (h *downlinkHandler) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	id := metadata.Destination.Fqdn
	if id == "" {
		id = metadata.Destination.AddrString()
	}
	if ctx == nil {
		ctx = h.ctx
	}
	return serveDownlink(ctx, id, conn)
}

func (h *downlinkHandler) NewPacketConnection(_ context.Context, conn N.PacketConn, _ M.Metadata) error {
	closer, ok := any(conn).(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

type nlogAdapter struct{}

func (nlogAdapter) Trace(...any) {}
func (nlogAdapter) Debug(...any) {}
func (nlogAdapter) Info(args ...any) {
	nlog.Core().Debug("xhttp-downlink", "msg", fmt.Sprint(args...))
}
func (nlogAdapter) Warn(args ...any) {
	nlog.Core().Warn("xhttp-downlink", "msg", fmt.Sprint(args...))
}
func (nlogAdapter) Error(args ...any) {
	nlog.Core().Error("xhttp-downlink", "msg", fmt.Sprint(args...))
}
func (nlogAdapter) Fatal(args ...any) {
	nlog.Core().Error("xhttp-downlink", "msg", fmt.Sprint(args...))
}
func (nlogAdapter) Panic(args ...any) {
	nlog.Core().Error("xhttp-downlink", "msg", fmt.Sprint(args...))
}

var _ logger.Logger = nlogAdapter{}

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "xhttp-downlink"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"xhttp-downlink"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
