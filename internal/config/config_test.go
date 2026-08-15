package config_test

import (
	"testing"

	"ytdlweb/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvDefaults(t *testing.T) {
	cfg := config.FromEnv(env(nil))
	if cfg.Port != 8080 || cfg.MaxConcurrent != 3 {
		t.Fatalf("Defaults falsch: %+v", cfg)
	}
	if cfg.OutputTemplate != "%(title)s [%(id)s].%(ext)s" {
		t.Fatalf("OUTPUT_TEMPLATE-Default falsch: %q", cfg.OutputTemplate)
	}
	if cfg.DownloadDir != "/downloads" || cfg.ConfigDir != "/config" || !cfg.UpdateOnStart {
		t.Fatalf("Defaults falsch: %+v", cfg)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	cfg := config.FromEnv(env(map[string]string{
		"PORT":                  "9090",
		"MAX_CONCURRENT":        "5",
		"OUTPUT_TEMPLATE":       "%(id)s.%(ext)s",
		"DOWNLOAD_DIR":          "/mnt/media",
		"CONFIG_DIR":            "/mnt/cfg",
		"YTDLP_UPDATE_ON_START": "false",
	}))
	if cfg.Port != 9090 || cfg.MaxConcurrent != 5 || cfg.UpdateOnStart {
		t.Fatalf("Overrides nicht übernommen: %+v", cfg)
	}
	if cfg.OutputTemplate != "%(id)s.%(ext)s" || cfg.DownloadDir != "/mnt/media" || cfg.ConfigDir != "/mnt/cfg" {
		t.Fatalf("Overrides nicht übernommen: %+v", cfg)
	}
}

func TestFromEnvInvalidNumbersFallBack(t *testing.T) {
	cfg := config.FromEnv(env(map[string]string{"PORT": "abc", "MAX_CONCURRENT": "-2"}))
	if cfg.Port != 8080 || cfg.MaxConcurrent != 3 {
		t.Fatalf("ungültige Zahlen müssen auf Defaults fallen: %+v", cfg)
	}
}

func TestFromEnvUpdateFlagVariants(t *testing.T) {
	for _, v := range []string{"false", "FALSE", "0", "no"} {
		if config.FromEnv(env(map[string]string{"YTDLP_UPDATE_ON_START": v})).UpdateOnStart {
			t.Fatalf("%q muss Update deaktivieren", v)
		}
	}
	if !config.FromEnv(env(map[string]string{"YTDLP_UPDATE_ON_START": "true"})).UpdateOnStart {
		t.Fatal("true muss Update aktivieren")
	}
}
