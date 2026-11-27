package splithttp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// writePacketUpSuccessResponse returns a panel-like JSON body for packet-up
// POST acks so middleboxes / observers see a normal API response instead of
// an empty 200. Clients discard the body.
func writePacketUpSuccessResponse(writer http.ResponseWriter) {
	// 64–192 random bytes → base64 looks like a real opaque payload.
	n := 64
	var rb [1]byte
	if _, err := rand.Read(rb[:]); err == nil {
		n = 64 + int(rb[0])%129
	}
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	data := base64.StdEncoding.EncodeToString(raw)

	body := fmt.Sprintf(
		"{\n    \"status\": \"success\",\n    \"message\": \"\\u64cd\\u4f5c\\u6210\\u529f\",\n    \"data\": \"%s\",\n    \"error\": null\n}",
		data,
	)
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, body)
}
