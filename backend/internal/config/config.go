package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	APIAddr                    string
	DatabaseURL                string
	StorageRoot                string
	ReviewLockTTLMinutes       int
	FrontendOrigin             string
	ReviewSessionHeader        string
	MaxMeetingMinutes          int
	MaxUploadBytes             int64
	FathomAPIKey               string
	FathomWebhookSecret        string
	FathomWebhookToleranceSecs int
	EnableAudioUploads         bool
}

func Load() (Config, error) {
	cfg := Config{
		APIAddr:                    defaultAPIAddr(),
		DatabaseURL:                os.Getenv("DATABASE_URL"),
		StorageRoot:                getenv("STORAGE_ROOT", "../data"),
		ReviewLockTTLMinutes:       getenvInt("REVIEW_LOCK_TTL_MINUTES", 20),
		FrontendOrigin:             getenv("FRONTEND_ORIGIN", "http://localhost:5173"),
		ReviewSessionHeader:        "X-Review-Session",
		MaxMeetingMinutes:          45,
		MaxUploadBytes:             500 << 20,
		FathomAPIKey:               os.Getenv("FATHOM_API_KEY"),
		FathomWebhookSecret:        os.Getenv("FATHOM_WEBHOOK_SECRET"),
		FathomWebhookToleranceSecs: getenvInt("FATHOM_WEBHOOK_TOLERANCE_SECONDS", 300),
		EnableAudioUploads:         getenvBool("ENABLE_AUDIO_UPLOADS", true),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	absRoot, err := filepath.Abs(cfg.StorageRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve storage root: %w", err)
	}
	cfg.StorageRoot = absRoot

	return cfg, nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func defaultAPIAddr() string {
	if value := strings.TrimSpace(os.Getenv("API_ADDR")); value != "" {
		return value
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return ":" + port
	}
	return ":8080"
}
