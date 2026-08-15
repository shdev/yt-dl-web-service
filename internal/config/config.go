// Package config liest die Laufzeit-Konfiguration aus Umgebungsvariablen (Spec §3).
package config

import (
	"strconv"
	"strings"
)

type Config struct {
	Port           int
	MaxConcurrent  int
	OutputTemplate string
	DownloadDir    string
	ConfigDir      string
	UpdateOnStart  bool
}

func FromEnv(getenv func(string) string) Config {
	cfg := Config{
		Port:           8080,
		MaxConcurrent:  3,
		OutputTemplate: "%(title)s [%(id)s].%(ext)s",
		DownloadDir:    "/downloads",
		ConfigDir:      "/config",
		UpdateOnStart:  true,
	}
	if n, err := strconv.Atoi(getenv("PORT")); err == nil && n > 0 {
		cfg.Port = n
	}
	if n, err := strconv.Atoi(getenv("MAX_CONCURRENT")); err == nil && n > 0 {
		cfg.MaxConcurrent = n
	}
	if v := getenv("OUTPUT_TEMPLATE"); v != "" {
		cfg.OutputTemplate = v
	}
	if v := getenv("DOWNLOAD_DIR"); v != "" {
		cfg.DownloadDir = v
	}
	if v := getenv("CONFIG_DIR"); v != "" {
		cfg.ConfigDir = v
	}
	switch strings.ToLower(getenv("YTDLP_UPDATE_ON_START")) {
	case "false", "0", "no":
		cfg.UpdateOnStart = false
	}
	return cfg
}
