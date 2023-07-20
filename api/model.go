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
		Replicas []Replica `json:"Replicas,omitempty"`
	}
	Replica struct {
		Provider     string    `json:"provider"`
		Status       string    `json:"status"`
		LastVerified time.Time `json:"lastVerified"`
		Expiration   time.Time `json:"expiration"`
	}
)
