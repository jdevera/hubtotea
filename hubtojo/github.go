package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v63/github"
)

type ListOptions struct {
	Affiliation string
	PerPage     int
	Page        int
}

type GithubRepositories struct {
	Owned   []*github.Repository
	Starred []*github.Repository
}

func RepositoryList(ctx context.Context, client *github.Client, user string, opt *ListOptions,
) ([]*github.Repository, *github.Response, error) {
	if opt == nil {
		opt = &ListOptions{}
	}
	if user == "" {
		_opts := &github.RepositoryListByAuthenticatedUserOptions{
			Affiliation: opt.Affiliation,
			ListOptions: github.ListOptions{
				PerPage: opt.PerPage,
				Page:    opt.Page,
			},
		}
		return client.Repositories.ListByAuthenticatedUser(ctx, _opts)
	}
	_opts := &github.RepositoryListByUserOptions{
		ListOptions: github.ListOptions{
			PerPage: opt.PerPage,
			Page:    opt.Page,
		},
	}
	return client.Repositories.ListByUser(ctx, user, _opts)
}

func GetGithubRepositories(ctx context.Context, config Config) (GithubRepositories, error) {
	return getGithubRepositories(ctx, github.NewClient(nil), config)
}

func getGithubRepositories(ctx context.Context, client *github.Client, config Config) (GithubRepositories, error) {
	client = authenticatedGithubClient(client, config)
	if config.MirrorPrivateRepos || config.MirrorStarredRepos {
		if err := validateGithubTokenUser(ctx, client, config.GithubUsername); err != nil {
			return GithubRepositories{}, err
		}
	}

	owned, err := listGithubOwnedRepos(ctx, client, config)
	if err != nil {
		return GithubRepositories{}, err
	}
	result := GithubRepositories{Owned: owned}
	if !config.MirrorStarredRepos {
		return result, nil
	}

	starred, err := listGithubStarredRepos(ctx, client)
	result.Starred = starred
	if err != nil {
		return result, err
	}
	return result, nil
}

func authenticatedGithubClient(client *github.Client, config Config) *github.Client {
	if config.GithubToken == nil {
		return client
	}
	return client.WithAuthToken(*config.GithubToken)
}

func validateGithubTokenUser(ctx context.Context, client *github.Client, expectedUsername string) error {
	authenticatedUser, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return fmt.Errorf("get authenticated GitHub user: %w", err)
	}
	if !strings.EqualFold(authenticatedUser.GetLogin(), expectedUsername) {
		return fmt.Errorf("GitHub token belongs to %q, expected %q", authenticatedUser.GetLogin(), expectedUsername)
	}
	return nil
}

func listGithubOwnedRepos(ctx context.Context, client *github.Client, config Config) ([]*github.Repository, error) {
	var repos []*github.Repository

	opt := &ListOptions{
		PerPage: 30,
	}

	username := config.GithubUsername
	if config.MirrorPrivateRepos {
		username = ""
		opt.Affiliation = "owner"
	}

	for {
		pageRepos, resp, err := RepositoryList(
			ctx, client, username, opt)
		if err != nil {
			return nil, err
		}
		repos = append(repos, pageRepos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	// Filter out repos that we are not mirroring
	if !config.MirrorForks || !config.MirrorPublicRepos || !config.MirrorPrivateRepos {
		var filteredRepos []*github.Repository
		for _, repo := range repos {
			if !config.MirrorForks && *repo.Fork {
				continue
			}
			if !config.MirrorPublicRepos && (!*repo.Private && !*repo.Fork) {
				continue
			}
			if !config.MirrorPrivateRepos && *repo.Private {
				continue
			}
			filteredRepos = append(filteredRepos, repo)
		}
		repos = filteredRepos
	}

	return repos, nil
}

func listGithubStarredRepos(ctx context.Context, client *github.Client) ([]*github.Repository, error) {
	var repos []*github.Repository
	opt := &github.ActivityListStarredOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		starred, resp, err := client.Activity.ListStarred(ctx, "", opt)
		if err != nil {
			return repos, fmt.Errorf("list starred GitHub repositories: %w", err)
		}
		for _, item := range starred {
			if item != nil && item.Repository != nil {
				repos = append(repos, item.Repository)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return repos, nil
}
