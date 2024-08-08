package main

import (
	"code.gitea.io/sdk/gitea"
	"context"
	"fmt"
	"github.com/google/go-github/v63/github"
	"log"
)

type MirrorResult int

const (
	Input MirrorResult = iota
	Created
	WouldCreate
	Skipped
	Failed
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
func GiteaMirror(ctx context.Context, githubRepo *github.Repository, config Config) (MirrorResult, error) {
	prefix := ""
	if workerId := ctx.Value("worker_id"); workerId != nil {
		prefix = fmt.Sprintf("[Worker %d] ", workerId)
	}
	client, err := gitea.NewClient(config.GiteaUrl,
		gitea.SetToken(config.GiteaToken),
		gitea.SetContext(ctx),
	)
	if err != nil {
		return Failed, err
	}
	giteaRepo, _, err := client.GetRepo(config.GiteaUsername, *githubRepo.Name)
	if err == nil {
		log.Printf("%sSkipping repository %s. It already exists on Gitea\n", prefix, giteaRepo.FullName)
		return Skipped, nil
	}
	if config.DryRun {
		log.Printf("%s[DRY-RUN] Would create repository %s on Gitea\n", prefix, *githubRepo.FullName)
		return WouldCreate, nil
	}
	log.Printf("%sCreating repository %s on Gitea\n", prefix, *githubRepo.FullName)

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
		return Failed, err
	}
	log.Printf("%sRepository %s created on Gitea\n", prefix, *githubRepo.FullName)
	return Created, nil
}
