package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v63/github"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSynchronizationRunIsPublishedByStatsEndpoint(t *testing.T) {
	githubServer := newIntegrationGithubServer()
	defer githubServer.Close()
	forgejoServer := newIntegrationForgejoServer()
	defer forgejoServer.Close()

	githubClient := github.NewClient(githubServer.Client())
	githubClient.BaseURL, _ = url.Parse(githubServer.URL + "/")
	listRepositories := func(ctx context.Context, config Config) (GithubRepositories, error) {
		return getGithubRepositories(ctx, githubClient, config)
	}
	config := Config{
		GithubUsername:    "source",
		ForgejoUrl:        forgejoServer.URL,
		ForgejoToken:      "forgejo-token",
		ForgejoUsername:   "owner",
		NumWorkers:        3,
		MirrorPublicRepos: true,
		RunTimeout:        time.Second,
	}
	store := NewStatsStore("integration-test", 0)
	metrics := NewMetrics("integration-test", 0)
	webServer, err := StartWebServer("127.0.0.1:0", store, metrics)
	if err != nil {
		t.Fatalf("start web server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := webServer.Shutdown(ctx); err != nil {
			t.Errorf("shut down web server: %v", err)
		}
	})

	synchronize := func(ctx context.Context, config Config) (RunStats, error) {
		return syncRepoList(ctx, config, listRepositories, EnsureStarredOrganization, ForgejoMirror)
	}
	runEvery(context.Background(), 0, runRecorders{store, metrics}, func(ctx context.Context, _ int) RunStats {
		return syncOnce(ctx, config, synchronize)
	})

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + webServer.Addr + "/stats")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var snapshot StatsSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if snapshot.Version != "integration-test" {
		t.Fatalf("version = %q, want %q", snapshot.Version, "integration-test")
	}
	if snapshot.CurrentRun != nil || snapshot.NextRunAt != nil {
		t.Fatal("one-shot synchronization did not finish cleanly")
	}
	assertIntegrationRunStats(t, snapshot.LastRun)
	assertIntegrationMetrics(t, webServer.Addr)
}

func TestStarredSynchronizationCreatesArchiveOrganizationAndMirror(t *testing.T) {
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer github-token" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"source"}`)
		case "/users/source/repos":
			fmt.Fprint(w, `[{"id":1,"name":"owned","full_name":"source/owned","clone_url":"https://github.test/source/owned.git","private":false,"fork":false}]`)
		case "/user/starred":
			fmt.Fprint(w, `[
				{"repo":{"id":1,"name":"owned","full_name":"source/owned","owner":{"login":"source"},"clone_url":"https://github.test/source/owned.git","private":false,"fork":false}},
				{"repo":{"id":2,"name":"project","full_name":"other/project","owner":{"login":"other"},"clone_url":"https://github.test/other/project.git","private":false,"fork":false}}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()

	type migrateRequest struct {
		RepoName  string `json:"repo_name"`
		RepoOwner string `json:"repo_owner"`
		CloneAddr string `json:"clone_addr"`
	}
	organizationCreated := make(chan struct{}, 1)
	migrations := make(chan migrateRequest, 1)
	forgejoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/version":
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/github-stars":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			organizationCreated <- struct{}{}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"username":"github-stars"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/owner/owned":
			fmt.Fprint(w, `{"full_name":"owner/owned"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/github-stars/other__project":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			var request migrateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, `{"message":"invalid request"}`, http.StatusBadRequest)
				return
			}
			migrations <- request
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"full_name":"github-stars/other__project"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer forgejoServer.Close()

	githubClient := github.NewClient(githubServer.Client())
	githubClient.BaseURL, _ = url.Parse(githubServer.URL + "/")
	listRepositories := func(ctx context.Context, config Config) (GithubRepositories, error) {
		return getGithubRepositories(ctx, githubClient, config)
	}
	githubToken := "github-token"
	stats, err := syncRepoList(context.Background(), Config{
		GithubUsername:          "source",
		GithubToken:             &githubToken,
		ForgejoUrl:              forgejoServer.URL,
		ForgejoToken:            "forgejo-token",
		ForgejoUsername:         "owner",
		NumWorkers:              1,
		MirrorPublicRepos:       true,
		MirrorStarredRepos:      true,
		StarredOrg:              "github-stars",
		MaxStarredCreatesPerRun: 25,
	}, listRepositories, EnsureStarredOrganization, ForgejoMirror)
	if err != nil {
		t.Fatalf("synchronize starred repositories: %v", err)
	}
	if stats.TotalRead != 2 || stats.OwnedDiscovered != 1 || stats.StarredDiscovered != 2 || stats.Duplicates != 1 {
		t.Fatalf("unexpected discovery stats: %+v", stats)
	}
	if stats.Created != 1 || stats.Skipped != 1 || stats.Failed != 0 || stats.StarredBacklog != 0 {
		t.Fatalf("unexpected result stats: %+v", stats)
	}
	if stats.StarredOrganization == nil || stats.StarredOrganization.Result != OrganizationCreated {
		t.Fatalf("unexpected organization stats: %+v", stats.StarredOrganization)
	}

	select {
	case <-organizationCreated:
	default:
		t.Fatal("archive organization was not created")
	}
	select {
	case request := <-migrations:
		want := migrateRequest{
			RepoName:  "other__project",
			RepoOwner: "github-stars",
			CloneAddr: "https://github.test/other/project.git",
		}
		if request != want {
			t.Fatalf("migration request = %+v, want %+v", request, want)
		}
	default:
		t.Fatal("starred repository was not migrated")
	}
}

func assertIntegrationMetrics(t *testing.T, addr string) {
	t.Helper()
	expected := strings.NewReader(`
# HELP hubtojo_repository_results_total Total number of repository synchronization results by outcome.
# TYPE hubtojo_repository_results_total counter
hubtojo_repository_results_total{result="created"} 1
hubtojo_repository_results_total{result="failed"} 1
hubtojo_repository_results_total{result="skipped"} 1
hubtojo_repository_results_total{result="would_create"} 0
# HELP hubtojo_runs_total Total number of completed synchronization runs by status.
# TYPE hubtojo_runs_total counter
hubtojo_runs_total{status="completed_with_errors"} 1
hubtojo_runs_total{status="error"} 0
hubtojo_runs_total{status="success"} 0
hubtojo_runs_total{status="unknown"} 0
`)
	if err := testutil.ScrapeAndCompare(
		"http://"+addr+"/metrics",
		expected,
		"hubtojo_repository_results_total",
		"hubtojo_runs_total",
	); err != nil {
		t.Fatalf("compare metrics: %v", err)
	}
}

func assertIntegrationRunStats(t *testing.T, stats *RunStats) {
	t.Helper()
	if stats == nil {
		t.Fatal("last run is not present")
	}
	if stats.Status != "completed_with_errors" {
		t.Fatalf("status = %q, want completed_with_errors", stats.Status)
	}
	if stats.TotalRead != 3 || stats.Created != 1 || stats.Skipped != 1 || stats.Failed != 1 {
		t.Fatalf("unexpected repository counts: %+v", *stats)
	}
	if len(stats.CreatedRepositories) != 1 || stats.CreatedRepositories[0] != "source/create-me" {
		t.Fatalf("created repositories = %v", stats.CreatedRepositories)
	}
	if len(stats.FailedRepositories) != 1 || stats.FailedRepositories[0].Name != "source/failing" {
		t.Fatalf("failed repositories = %v", stats.FailedRepositories)
	}
	if stats.FinishedAt == nil || stats.StartedAt.IsZero() || stats.DurationSeconds < 0 {
		t.Fatal("run timing information is incomplete")
	}
}

func newIntegrationGithubServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/users/source/repos" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[
			{"name":"create-me","full_name":"source/create-me","clone_url":"https://github.test/source/create-me.git","private":false,"fork":false},
			{"name":"existing","full_name":"source/existing","clone_url":"https://github.test/source/existing.git","private":false,"fork":false},
			{"name":"failing","full_name":"source/failing","clone_url":"https://github.test/source/failing.git","private":false,"fork":false}
		]`)
	}))
}

func newIntegrationForgejoServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case "/api/v1/repos/owner/create-me":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
		case "/api/v1/repos/owner/existing":
			fmt.Fprint(w, `{"full_name":"owner/existing"}`)
		case "/api/v1/repos/owner/failing":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"backend unavailable"}`)
		case "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"full_name":"owner/create-me"}`)
		default:
			http.NotFound(w, r)
		}
	}))
}
