package test

import (
	"os"
	"testing"
)

type (
	Environment struct {
		t                      *testing.T
		MotionAPIEndpoint      string
		MotionWalletAddress    string
		MotionWalletKey        string
		MotionStorageProvider  string
		SingularityAPIEndpoint string
	}
)

func NewEnvironment(t *testing.T) *Environment {
	if os.Getenv("MOTION_INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration tests. " +
			"To run integration tests start up dependant APIs and set MOTION_INTEGRATION_TEST environment variable to `true`.")
		return nil
	}

	return &Environment{
		t:                      t,
		MotionAPIEndpoint:      getEnvOrDefault("MOTION_API_ENDPOINT", "http://localhost:40080"),
		MotionWalletAddress:    getEnvOrDefault("MOTION_WALLET_ADDR", ""),
		MotionWalletKey:        getEnvOrDefault("MOTION_WALLET_KEY", ""),
		MotionStorageProvider:  getEnvOrDefault("MOTION_STORAGE_PROVIDERS", ""),
		SingularityAPIEndpoint: getEnvOrDefault("SINGULARITY_API_ENDPOINT", "http://localhost:9090"),
	}
}

func getEnvOrDefault(key, def string) string {
	switch v, ok := os.LookupEnv(key); {
	case ok:
		return v
	default:
		return def
	}
}
