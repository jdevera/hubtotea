package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMakeConfigFromEnvResolvesCompleteConfiguration(t *testing.T) {
	forgejoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "token forgejo-token" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case "/api/v1/user":
			fmt.Fprint(w, `{"login":"forgejo-owner"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer forgejoServer.Close()

	t.Setenv("GITHUB_USERNAME", "source-user")
	t.Setenv("FORGEJO_URL", forgejoServer.URL)
	t.Setenv("FORGEJO_TOKEN", "forgejo-token")
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("HUBTOJO_NUM_WORKERS", "7")
	t.Setenv("HUBTOJO_MIRROR_PUBLIC_REPOS", "false")
	t.Setenv("HUBTOJO_MIRROR_PRIVATE_REPOS", "true")
	t.Setenv("HUBTOJO_MIRROR_FORKS", "true")
	t.Setenv("HUBTOJO_MIRROR_STARRED_REPOS", "true")
	t.Setenv("HUBTOJO_STARRED_ORG", "star-archive")
	t.Setenv("HUBTOJO_MAX_STARRED_CREATES_PER_RUN", "11")
	t.Setenv("HUBTOJO_DRY_RUN", "true")
	t.Setenv("HUBTOJO_SYNC_INTERVAL", "42")
	t.Setenv("HUBTOJO_RUN_TIMEOUT", "17")
	t.Setenv("HUBTOJO_WEB_ADDR", "127.0.0.1:9090")

	config, err := MakeConfigFromEnv()
	if err != nil {
		t.Fatalf("make config: %v", err)
	}
	if config.GithubUsername != "source-user" || config.ForgejoUrl != forgejoServer.URL {
		t.Fatalf("source configuration was not loaded: %+v", config)
	}
	if config.ForgejoToken != "forgejo-token" || config.ForgejoUsername != "forgejo-owner" {
		t.Fatalf("Forgejo configuration was not resolved: %+v", config)
	}
	if config.GithubToken == nil || *config.GithubToken != "github-token" {
		t.Fatal("GitHub token was not loaded")
	}
	if config.NumWorkers != 7 || config.SyncInterval != 42 || config.RunTimeout != 17*time.Second || config.MaxStarredCreatesPerRun != 11 {
		t.Fatalf("numeric configuration was not loaded: %+v", config)
	}
	if config.MirrorPublicRepos || !config.MirrorPrivateRepos || !config.MirrorForks || !config.MirrorStarredRepos || !config.DryRun {
		t.Fatalf("boolean configuration was not loaded: %+v", config)
	}
	if config.StarredOrg != "star-archive" {
		t.Fatalf("starred organization = %q, want star-archive", config.StarredOrg)
	}
	if config.WebAddr != "127.0.0.1:9090" {
		t.Fatalf("web address = %q, want 127.0.0.1:9090", config.WebAddr)
	}
}

func TestRunContextUsesConfiguredTimeout(t *testing.T) {
	config := Config{RunTimeout: 10 * time.Millisecond}
	ctx, cancel := config.withRunTimeout(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("run context did not reach its deadline")
	}
}

func TestConfigValidateRejectsUnsafeNumericValues(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name:      "zero workers",
			configure: func(config *Config) { config.NumWorkers = 0 },
			wantError: "HUBTOJO_NUM_WORKERS",
		},
		{
			name:      "negative sync interval",
			configure: func(config *Config) { config.SyncInterval = -1 },
			wantError: "HUBTOJO_SYNC_INTERVAL",
		},
		{
			name:      "zero run timeout",
			configure: func(config *Config) { config.RunTimeout = 0 },
			wantError: "HUBTOJO_RUN_TIMEOUT",
		},
		{
			name:      "negative starred create limit",
			configure: func(config *Config) { config.MaxStarredCreatesPerRun = -1 },
			wantError: "HUBTOJO_MAX_STARRED_CREATES_PER_RUN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validConfig()
			tt.configure(&config)
			err := config.validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validation error = %v, want %s", err, tt.wantError)
			}
		})
	}
}

func TestConfigValidateRejectsEmptyPrivateRepoToken(t *testing.T) {
	config := validConfig()
	emptyToken := ""
	config.GithubToken = &emptyToken
	config.MirrorPrivateRepos = true

	err := config.validate()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("validation error = %v, want GITHUB_TOKEN error", err)
	}
}

func TestConfigValidateRequiresTokenAndOrganizationForStarredRepos(t *testing.T) {
	config := validConfig()
	config.MirrorStarredRepos = true
	config.StarredOrg = ""

	err := config.validate()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "HUBTOJO_STARRED_ORG") {
		t.Fatalf("validation error = %v, want token and organization errors", err)
	}
}

func validConfig() Config {
	return Config{
		GithubUsername: "source-user",
		ForgejoUrl:     "https://forgejo.example.com",
		ForgejoToken:   "token",
		NumWorkers:     5,
		SyncInterval:   3600,
		RunTimeout:     time.Hour,
	}
}
