package api

import "time"

type (
	// PostBlobResponse represents the response to a successful POST request to upload a blob.
	PostBlobResponse struct {
		// ID is the unique identifier for the uploaded blob.
		ID string `json:"id"`
	}
	// ErrorResponse represents the response that signal an error has occurred.
	ErrorResponse struct {
		// Error is the description of the error.
		Error string `json:"error"`
	}
	GetStatusResponse struct {
		ID       string    `json:"id"`
		Replicas []Replica `json:"replicas,omitempty"`
	}
	Replica struct {
		Provider string  `json:"provider"`
		Pieces   []Piece `json:"pieces"`
	}
	Piece struct {
		Id           int64     `json:"id"`
		Expiration   time.Time `json:"expiration"`
		LastVerified time.Time `json:"lastVerified"`
		PieceCID     string    `json:"pieceCid"`
		Status       string    `json:"status"`
	}
)
