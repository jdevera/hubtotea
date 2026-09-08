package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v63/github"
)

func TestGithubRepositoriesArePaginatedAndFiltered(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/users/source/repos" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/users/source/repos?page=2>; rel="next"`, server.URL))
			fmt.Fprint(w, `[
				{"name":"public-one","full_name":"source/public-one","private":false,"fork":false},
				{"name":"fork","full_name":"source/fork","private":false,"fork":true}
			]`)
		case "2":
			fmt.Fprint(w, `[
				{"name":"public-two","full_name":"source/public-two","private":false,"fork":false}
			]`)
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	repositories, err := getGithubOwnedRepositoriesForTest(context.Background(), client, Config{
		GithubUsername:    "source",
		MirrorPublicRepos: true,
	})
	if err != nil {
		t.Fatalf("get repositories: %v", err)
	}

	names := repositoryNames(repositories)
	want := []string{"source/public-one", "source/public-two"}
	if !slices.Equal(names, want) {
		t.Fatalf("repositories = %v, want %v", names, want)
	}
}

func TestGithubRepositoriesAreFilteredByConfiguredTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"source"}`)
		case "/user/repos", "/users/source/repos":
			fmt.Fprint(w, `[
				{"name":"public","full_name":"source/public","private":false,"fork":false},
				{"name":"private","full_name":"source/private","private":true,"fork":false},
				{"name":"fork","full_name":"source/fork","private":false,"fork":true}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	token := "github-token"
	tests := []struct {
		name   string
		config Config
		want   []string
	}{
		{
			name: "public repositories",
			config: Config{
				GithubUsername:    "source",
				MirrorPublicRepos: true,
			},
			want: []string{"source/public"},
		},
		{
			name: "forks",
			config: Config{
				GithubUsername: "source",
				MirrorForks:    true,
			},
			want: []string{"source/fork"},
		},
		{
			name: "private repositories",
			config: Config{
				GithubUsername:     "source",
				GithubToken:        &token,
				MirrorPrivateRepos: true,
			},
			want: []string{"source/private"},
		},
		{
			name: "all repository types",
			config: Config{
				GithubUsername:     "source",
				GithubToken:        &token,
				MirrorPublicRepos:  true,
				MirrorPrivateRepos: true,
				MirrorForks:        true,
			},
			want: []string{"source/public", "source/private", "source/fork"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repositories, err := getGithubOwnedRepositoriesForTest(context.Background(), client, tt.config)
			if err != nil {
				t.Fatalf("get repositories: %v", err)
			}
			names := repositoryNames(repositories)
			if !slices.Equal(names, tt.want) {
				t.Fatalf("repositories = %v, want %v", names, tt.want)
			}
		})
	}
}

func repositoryNames(repositories []*github.Repository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.GetFullName())
	}
	return names
}

func TestGithubTokenAuthenticatesPublicRepositoryRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer github-token" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	token := "github-token"
	_, err := getGithubOwnedRepositoriesForTest(context.Background(), client, Config{
		GithubUsername:    "source",
		GithubToken:       &token,
		MirrorPublicRepos: true,
	})
	if err != nil {
		t.Fatalf("get repositories: %v", err)
	}
}

func TestPrivateRepoTokenMustMatchConfiguredUser(t *testing.T) {
	tests := []struct {
		name      string
		login     string
		wantError bool
	}{
		{name: "matching user", login: "SourceUser"},
		{name: "case insensitive match", login: "sourceuser"},
		{name: "different user", login: "another-user", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user":
					fmt.Fprintf(w, `{"login":%q}`, tt.login)
				case "/user/repos":
					fmt.Fprint(w, `[]`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client := github.NewClient(server.Client())
			client.BaseURL, _ = url.Parse(server.URL + "/")
			token := "token"
			_, err := getGithubOwnedRepositoriesForTest(context.Background(), client, Config{
				GithubUsername:     "SourceUser",
				GithubToken:        &token,
				MirrorPrivateRepos: true,
			})
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "GitHub token belongs to") {
					t.Fatalf("error = %v, want token owner mismatch", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("get repositories: %v", err)
			}
		})
	}
}

func TestGithubRepositoriesIncludePaginatedStarredRepositoriesWithoutTypeFilters(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer github-token" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"source"}`)
		case "/users/source/repos":
			fmt.Fprint(w, `[{"id":1,"name":"owned","full_name":"source/owned","private":false,"fork":false}]`)
		case "/user/starred":
			switch r.URL.Query().Get("page") {
			case "":
				w.Header().Set("Link", fmt.Sprintf(`<%s/user/starred?page=2>; rel="next"`, server.URL))
				fmt.Fprint(w, `[
					{"repo":{"id":2,"name":"private","full_name":"other/private","private":true,"fork":false}},
					{"repo":{"id":3,"name":"fork","full_name":"other/fork","private":false,"fork":true}}
				]`)
			case "2":
				fmt.Fprint(w, `[{"repo":{"id":4,"name":"archived","full_name":"other/archived","private":false,"fork":false,"archived":true}}]`)
			default:
				http.Error(w, "unexpected page", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	token := "github-token"
	repositories, err := getGithubRepositories(context.Background(), client, Config{
		GithubUsername:     "source",
		GithubToken:        &token,
		MirrorPublicRepos:  true,
		MirrorStarredRepos: true,
	})
	if err != nil {
		t.Fatalf("get repositories: %v", err)
	}
	if want := []string{"source/owned"}; !slices.Equal(repositoryNames(repositories.Owned), want) {
		t.Fatalf("owned repositories = %v, want %v", repositoryNames(repositories.Owned), want)
	}
	wantStarred := []string{"other/private", "other/fork", "other/archived"}
	if !slices.Equal(repositoryNames(repositories.Starred), wantStarred) {
		t.Fatalf("starred repositories = %v, want %v", repositoryNames(repositories.Starred), wantStarred)
	}
}

func TestGithubRepositoriesDoNotCallStarredOrAuthenticatedEndpointsByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/users/source/repos" {
			t.Errorf("unexpected legacy-config request to %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	token := "github-token"
	_, err := getGithubRepositories(context.Background(), client, Config{
		GithubUsername:    "source",
		GithubToken:       &token,
		MirrorPublicRepos: true,
	})
	if err != nil {
		t.Fatalf("get repositories: %v", err)
	}
}

func getGithubOwnedRepositoriesForTest(ctx context.Context, client *github.Client, config Config) ([]*github.Repository, error) {
	repositories, err := getGithubRepositories(ctx, client, config)
	return repositories.Owned, err
}
