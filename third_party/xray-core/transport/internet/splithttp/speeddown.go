package splithttp

import (
	"net/http"
	"strconv"
	"strings"
)

// Cloudflare-compatible download endpoint for raw link / camouflage speed tests.
// Example: GET /__down?during=download&bytes=104857600
const (
	defaultSpeedDownPath = "/__down"
	defaultSpeedDownSize = int64(1 << 20)   // 1 MiB when bytes= omitted
	maxSpeedDownBytes    = int64(5 << 30) // 5 GiB hard cap
	speedDownChunkSize   = 64 << 10         // 64 KiB write chunks
)

// tryServeSpeedDown serves a fixed-size octet-stream like Cloudflare's
// speed.cloudflare.com/__down. Returns true when the request was handled.
func tryServeSpeedDown(writer http.ResponseWriter, request *http.Request, configuredPath string) bool {
	path := configuredPath
	if path == "" {
		path = defaultSpeedDownPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	reqPath := strings.TrimSuffix(request.URL.Path, "/")
	if reqPath != path {
		return false
	}

	switch request.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodOptions:
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "*")
		writer.WriteHeader(http.StatusNoContent)
		return true
	default:
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}

	n := defaultSpeedDownSize
	if raw := request.URL.Query().Get("bytes"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writer.WriteHeader(http.StatusBadRequest)
			return true
		}
		n = parsed
	}
	if n > maxSpeedDownBytes {
		n = maxSpeedDownBytes
	}

	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead || n == 0 {
		return true
	}

	buf := make([]byte, speedDownChunkSize)
	left := n
	for left > 0 {
		chunk := int64(len(buf))
		if chunk > left {
			chunk = left
		}
		if _, err := writer.Write(buf[:chunk]); err != nil {
			return true
		}
		if f, ok := writer.(http.Flusher); ok {
			f.Flush()
		}
		left -= chunk
	}
	return true
}
