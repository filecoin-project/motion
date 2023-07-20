package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/filecoin-project/motion/api"
)

func httpHeaderContentTypeJson() (string, string) {
	return "Content-Type", "application/json; charset=utf-8"
}
func httpHeaderContentTypeOctetStream() (string, string) {
	return "Content-Type", "application/octet-stream"
}

func httpHeaderContentTypeOptionsNoSniff() (string, string) {
	return "X-Content-Type-Options", "nosniff"
}

func httpHeaderContentLength(length uint64) (string, string) {
	return "Content-Length", strconv.FormatUint(length, 10)
}

func httpHeaderAllow(methods ...string) (string, string) {
	return "Allow", strings.Join(methods, ",")
}

func respondWithJson(w http.ResponseWriter, resp any, code int) {
	w.Header().Set(httpHeaderContentTypeJson())
	w.Header().Set(httpHeaderContentTypeOptionsNoSniff())
	if code != http.StatusOK {
		w.WriteHeader(code)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorw("Failed to encode response.", "code", code, "resp", resp, "err", err)
	}
}

func respondWithNotAllowed(w http.ResponseWriter, allowedMethods ...string) {
	w.Header().Set(httpHeaderAllow(allowedMethods...))
	respondWithJson(w, api.ErrorResponse{
		Error: `Method not allowed. Please see "Allow" response header for the list of allowed methods.`,
	}, http.StatusMethodNotAllowed)
}
