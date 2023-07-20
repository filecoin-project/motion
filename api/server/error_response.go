package server

import (
	"fmt"

	"github.com/filecoin-project/motion/api"
)

var (
	errResponsePageNotFound         = api.ErrorResponse{Error: "404 Page Not found"}
	errResponseInvalidBlobID        = api.ErrorResponse{Error: "Invalid blob ID"}
	errResponseBlobNotFound         = api.ErrorResponse{Error: "No blob is found for the given ID"}
	errResponseNotStreamContentType = api.ErrorResponse{Error: `Invalid content type, expected "application/octet-stream".`}
	errResponseInvalidContentLength = api.ErrorResponse{Error: "Invalid content length, expected unsigned numerical value."}
)

func errResponseInternalError(err error) api.ErrorResponse {
	return api.ErrorResponse{Error: fmt.Sprintf("Internal error occurred: %s", err.Error())}
}

func errResponseContentLengthTooLarge(max uint64) api.ErrorResponse {
	return api.ErrorResponse{Error: fmt.Sprintf(`Content-Length exceeds the maximum accepted content length of %d bytes.`, max)}
}
func errResponseMaxBlobLengthExceeded(max uint64) api.ErrorResponse {
	return api.ErrorResponse{Error: fmt.Sprintf(`Blob length exceeds the maximum accepted length of %d bytes.`, max)}
}
