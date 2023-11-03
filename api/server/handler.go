package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/filecoin-project/motion/api"
	"github.com/filecoin-project/motion/blob"
)

func (m *HttpServer) handleBlobRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodPost, http.MethodOptions))
	case http.MethodPost:
		m.handlePostBlob(w, r)
	default:
		respondWithNotAllowed(w, http.MethodPost, http.MethodOptions)
	}
}

func (m *HttpServer) handlePostBlob(w http.ResponseWriter, r *http.Request) {
	// TODO: check Accept header accepts JSON response
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		respondWithJson(w, errResponseNotStreamContentType, http.StatusBadRequest)
		return
	}
	body := r.Body
	var contentLength uint64
	if value := r.Header.Get("Content-Length"); value != "" {
		var err error
		if contentLength, err = strconv.ParseUint(value, 10, 32); err != nil {
			if errors.Is(err, strconv.ErrSyntax) {
				respondWithJson(w, errResponseInvalidContentLength, http.StatusBadRequest)
			} else {
				respondWithJson(w, errResponseContentLengthTooLarge(m.maxBlobLength), http.StatusBadRequest)
			}
			return
		}
		if contentLength > m.maxBlobLength {
			respondWithJson(w, errResponseContentLengthTooLarge(m.maxBlobLength), http.StatusBadRequest)
			return
		}
		// Wrap body reader to signal content length to upstream components.
		body = sizerReadCloser{
			ReadCloser: r.Body,
			size:       int64(contentLength),
		}
	}
	defer body.Close()
	desc, err := m.store.Put(r.Context(), body)
	switch err {
	case nil:
	case blob.ErrBlobTooLarge:
		respondWithJson(w, errResponseMaxBlobLengthExceeded(m.maxBlobLength), http.StatusBadRequest)
		return
	default:
		respondWithJson(w, errResponseInternalError(err), http.StatusInternalServerError)
		return
	}
	logger := logger.With("id", desc.ID, "size", desc.Size)
	if contentLength != 0 && desc.Size != contentLength {
		logger.Warnw("Content-Length in request header did not match the data length", "expectedSize", contentLength)
		// TODO: add option to reject such requests?
	}
	respondWithJson(w, api.PostBlobResponse{ID: desc.ID.String()}, http.StatusCreated)
	logger.Debugw("Blob crated successfully", "id", desc.ID, "size", desc.Size)
}

func (m *HttpServer) handleBlobSubtree(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodGet, http.MethodOptions))
	case http.MethodGet:
		m.handleBlobGet(w, r)
	default:
		respondWithNotAllowed(w, http.MethodPost, http.MethodOptions)
	}
}

func (m *HttpServer) handleBlobGet(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/v0/blob/")
	segments := strings.Split(suffix, "/")
	switch len(segments) {
	case 1:
		m.handleBlobGetByID(w, r, segments[0])
	case 2:
		if segments[1] == "status" {
			m.handleBlobGetStatusByID(w, r, segments[0])
		} else {
			respondWithJson(w, errResponsePageNotFound, http.StatusNotFound)
		}
	default:
		respondWithJson(w, errResponsePageNotFound, http.StatusNotFound)
	}
}

func (m *HttpServer) handleBlobGetByID(w http.ResponseWriter, r *http.Request, idUriSegment string) {
	var id blob.ID
	if err := id.Decode(idUriSegment); err != nil {
		respondWithJson(w, errResponseInvalidBlobID, http.StatusBadRequest)
		return
	}
	logger := logger.With("id", id)
	blobDesc, err := m.store.Describe(r.Context(), id)
	switch err {
	case nil:
	case blob.ErrBlobNotFound:
		respondWithJson(w, errResponseBlobNotFound, http.StatusNotFound)
		return
	default:
		respondWithJson(w, errResponseInternalError(err), http.StatusInternalServerError)
		return
	}
	if pass, ok := m.store.(blob.PassThroughGet); ok {
		pass.PassGet(w, r, id)
		return
	}
	blobReader, err := m.store.Get(r.Context(), id)
	switch err {
	case nil:
	case blob.ErrBlobNotFound:
		respondWithJson(w, errResponseBlobNotFound, http.StatusNotFound)
		return
	default:
		respondWithJson(w, errResponseInternalError(err), http.StatusInternalServerError)
		return
	}
	defer blobReader.Close()
	w.Header().Set(httpHeaderContentTypeOctetStream())
	w.Header().Set(httpHeaderContentTypeOptionsNoSniff())
	w.Header().Set(httpHeaderContentLength(blobDesc.Size))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachement; filename="%s.bin"`, id.String()))
	http.ServeContent(w, r, "", blobDesc.ModificationTime, blobReader)
	logger.Debug("Blob fetched successfully")
}

func (m *HttpServer) handleBlobGetStatusByID(w http.ResponseWriter, r *http.Request, idUriSegment string) {
	var id blob.ID
	if err := id.Decode(idUriSegment); err != nil {
		respondWithJson(w, errResponseInvalidBlobID, http.StatusBadRequest)
		return
	}
	logger := logger.With("id", id)
	blobDesc, err := m.store.Describe(r.Context(), id)
	switch err {
	case nil:
	case blob.ErrBlobNotFound:
		respondWithJson(w, errResponseBlobNotFound, http.StatusNotFound)
		return
	default:
		logger.Errorw("Failed to get status for ID", "err", err)
		respondWithJson(w, errResponseInternalError(err), http.StatusInternalServerError)
		return
	}

	response := api.GetStatusResponse{
		ID: idUriSegment,
	}

	if len(blobDesc.Replicas) != 0 {
		response.Replicas = make([]api.Replica, 0, len(blobDesc.Replicas))
		for _, replica := range blobDesc.Replicas {
			apiPieces := make([]api.Piece, 0, len(replica.Pieces))
			for _, piece := range replica.Pieces {
				apiPieces = append(apiPieces, api.Piece{
					Expiration:   piece.Expiration,
					LastVerified: piece.LastUpdated,
					PieceCID:     piece.PieceCID,
					Status:       piece.Status,
				})
			}
			response.Replicas = append(response.Replicas, api.Replica{
				Provider: replica.Provider,
				Pieces:   apiPieces,
			})
		}
	}
	respondWithJson(w, response, http.StatusOK)
}

func (m *HttpServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodOptions))
	default:
		respondWithJson(w, errResponsePageNotFound, http.StatusNotFound)
	}
}
