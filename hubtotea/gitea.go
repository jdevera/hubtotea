package main

import (
	"code.gitea.io/sdk/gitea"
	"context"
	"github.com/google/go-github/v63/github"
	"log"
)

func GiteaGetUsername(client *gitea.Client) (string, error) {
	user, _, err := client.GetMyUserInfo()
	if err != nil {
		return "", err
	}
	return user.UserName, nil
}

func GiteaClient(ctx context.Context, config Config) (*gitea.Client, error) {
	return gitea.NewClient(config.GiteaUrl, gitea.SetToken(config.GiteaToken), gitea.SetContext(ctx))
}

// GiteaMirror creates a repository on Gitea for the given GitHub repository. The
// repository is created with the same name and description as the GitHub
// repository. The repository is created as a mirror of the GitHub repository.
func GiteaMirror(ctx context.Context, githubRepo *github.Repository, config Config) error {
	client, err := gitea.NewClient(config.GiteaUrl,
		gitea.SetToken(config.GiteaToken),
		gitea.SetContext(ctx),
	)
	if err != nil {
		return err
	}
	giteaRepo, _, err := client.GetRepo(config.GiteaUsername, *githubRepo.Name)
	if err == nil {
		log.Printf("Skipping repository %s. It already exists on Gitea\n", giteaRepo.FullName)
		return nil
	}
	log.Printf("Creating repository %s on Gitea\n", *githubRepo.FullName)

	githubAuth := ""
	if config.GithubToken != nil {
		githubAuth = *config.GithubToken
	}

	option := gitea.MigrateRepoOption{
		AuthToken: githubAuth,
		CloneAddr: *githubRepo.CloneURL,
		RepoName:  *githubRepo.Name,
		RepoOwner: config.GiteaUsername,
		Private:   *githubRepo.Private,
		Mirror:    true,
	}
	if githubRepo.Description != nil {
		option.Description = *githubRepo.Description
	}
	_, _, err = client.MigrateRepo(option)
	if err != nil {
		return err
	}
	log.Printf("Repository %s created on Gitea\n", *githubRepo.FullName)
	return nil
}
