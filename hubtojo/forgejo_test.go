package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v63/github"
)

func TestForgejoMirrorMigratesMissingRepository(t *testing.T) {
	type migrateRequest struct {
		RepoName    string `json:"repo_name"`
		RepoOwner   string `json:"repo_owner"`
		CloneAddr   string `json:"clone_addr"`
		AuthToken   string `json:"auth_token"`
		Mirror      bool   `json:"mirror"`
		Private     bool   `json:"private"`
		Description string `json:"description"`
	}
	migrations := make(chan migrateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "token forgejo-token" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/version":
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/owner/repository":
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
			fmt.Fprint(w, `{"full_name":"owner/repository"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	githubToken := "github-token"
	repository := testGithubRepository()
	repository.Private = github.Bool(true)
	repository.Description = github.String("Repository description")
	result, err := ForgejoMirror(context.Background(), ownedRepositoryPlan(repository), Config{
		ForgejoUrl:      server.URL,
		ForgejoToken:    "forgejo-token",
		ForgejoUsername: "owner",
		GithubToken:     &githubToken,
	}, newCreationLimiter(25))
	if err != nil {
		t.Fatalf("mirror repository: %v", err)
	}
	if result != Created {
		t.Fatalf("result = %v, want %v", result, Created)
	}

	select {
	case request := <-migrations:
		want := migrateRequest{
			RepoName:    "repository",
			RepoOwner:   "owner",
			CloneAddr:   "https://github.test/source/repository.git",
			AuthToken:   "github-token",
			Mirror:      true,
			Private:     true,
			Description: "Repository description",
		}
		if request != want {
			t.Fatalf("migration request = %+v, want %+v", request, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Forgejo did not receive a migration request")
	}
}

func TestForgejoMirrorDryRunTreatsNotFoundAsMissing(t *testing.T) {
	server := newForgejoTestServer(t, http.StatusNotFound)
	defer server.Close()

	result, err := ForgejoMirror(context.Background(), ownedRepositoryPlan(testGithubRepository()), Config{
		ForgejoUrl:      server.URL,
		ForgejoUsername: "owner",
		DryRun:          true,
	}, newCreationLimiter(25))
	if err != nil {
		t.Fatalf("mirror repository: %v", err)
	}
	if result != WouldCreate {
		t.Fatalf("result = %v, want %v", result, WouldCreate)
	}
}

func TestForgejoMirrorDryRunReturnsLookupErrors(t *testing.T) {
	server := newForgejoTestServer(t, http.StatusInternalServerError)
	defer server.Close()

	result, err := ForgejoMirror(context.Background(), ownedRepositoryPlan(testGithubRepository()), Config{
		ForgejoUrl:      server.URL,
		ForgejoUsername: "owner",
		DryRun:          true,
	}, newCreationLimiter(25))
	if err == nil {
		t.Fatal("expected repository lookup error")
	}
	if result != Failed {
		t.Fatalf("result = %v, want %v", result, Failed)
	}
}

func TestForgejoMirrorOnlyConsumesStarredLimitForMissingRepositories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case "/api/v1/repos/github-stars/other__existing":
			fmt.Fprint(w, `{"full_name":"github-stars/other__existing"}`)
		case "/api/v1/repos/github-stars/other__missing", "/api/v1/repos/github-stars/other__deferred":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := Config{ForgejoUrl: server.URL, DryRun: true}
	limiter := newCreationLimiter(1)
	tests := []struct {
		name       string
		repository string
		want       MirrorResult
	}{
		{name: "existing repository", repository: "existing", want: Skipped},
		{name: "first missing repository", repository: "missing", want: WouldCreate},
		{name: "second missing repository", repository: "deferred", want: Deferred},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ForgejoMirror(context.Background(), RepositoryPlan{
				Repository:       githubRepository(1, "other", tt.repository),
				Source:           StarredRepositorySource,
				DestinationOwner: "github-stars",
				DestinationName:  "other__" + tt.repository,
			}, config, limiter)
			if err != nil {
				t.Fatalf("mirror repository: %v", err)
			}
			if result != tt.want {
				t.Fatalf("result = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestEnsureStarredOrganization(t *testing.T) {
	tests := []struct {
		name       string
		exists     bool
		isOwner    bool
		dryRun     bool
		wantResult OrganizationResult
		wantCreate bool
		wantError  string
	}{
		{
			name:       "existing organization owned by configured user",
			exists:     true,
			isOwner:    true,
			wantResult: OrganizationExisting,
		},
		{
			name:       "existing organization without permission",
			exists:     true,
			wantResult: OrganizationFailed,
			wantError:  "is not an owner",
		},
		{
			name:       "missing organization",
			wantResult: OrganizationCreated,
			wantCreate: true,
		},
		{
			name:       "dry run with missing organization",
			dryRun:     true,
			wantResult: OrganizationWouldCreate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/version":
					fmt.Fprint(w, `{"version":"11.0.10"}`)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/github-stars":
					if !tt.exists {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"message":"not found"}`)
						return
					}
					fmt.Fprint(w, `{"username":"github-stars"}`)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/owner/orgs/github-stars/permissions":
					fmt.Fprintf(w, `{"is_owner":%t}`, tt.isOwner)
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
					created = true
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, `{"username":"github-stars"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			result, err := EnsureStarredOrganization(context.Background(), Config{
				ForgejoUrl:      server.URL,
				ForgejoToken:    "forgejo-token",
				ForgejoUsername: "owner",
				StarredOrg:      "github-stars",
				DryRun:          tt.dryRun,
			})
			if result != tt.wantResult {
				t.Fatalf("result = %q, want %q", result, tt.wantResult)
			}
			if tt.wantError == "" && err != nil {
				t.Fatalf("ensure organization: %v", err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
			if created != tt.wantCreate {
				t.Fatalf("organization created = %t, want %t", created, tt.wantCreate)
			}
		})
	}
}

func newForgejoTestServer(t *testing.T, repoStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"version":"11.0.10"}`)
		case "/api/v1/repos/owner/repository":
			w.WriteHeader(repoStatus)
			fmt.Fprint(w, `{"message":"test response"}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func testGithubRepository() *github.Repository {
	return &github.Repository{
		Name:     github.String("repository"),
		FullName: github.String("source/repository"),
		CloneURL: github.String("https://github.test/source/repository.git"),
		Private:  github.Bool(false),
		Fork:     github.Bool(false),
	}
}

func ownedRepositoryPlan(repository *github.Repository) RepositoryPlan {
	return RepositoryPlan{
		Repository:       repository,
		Source:           OwnedRepositorySource,
		DestinationOwner: "owner",
		DestinationName:  repository.GetName(),
	}
}
