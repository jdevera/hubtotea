package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
)

type Config struct {
	GithubUsername     string
	GiteaUrl           string
	GiteaToken         string
	GiteaUsername      string
	GithubToken        *string
	NumWorkers         int
	MirrorPublicRepos  bool
	MirrorPrivateRepos bool
	MirrorForks        bool
	DryRun             bool
	SyncInterval       int
}

func (c *Config) log() {
	log.Printf("Configuration:\n")
	log.Printf("  Github Username: %s\n", c.GithubUsername)
	log.Printf("  Gitea URL: %s\n", c.GiteaUrl)
	log.Printf("  Gitea Token: ****\n")
	log.Printf("  Gitea Username: %s\n", c.GiteaUsername)
	if c.GithubToken != nil {
		log.Printf("  Github Token: ****\n") // Dereference pointer to print value
	} else {
		log.Printf("  Github Token: Not provided\n")
	}
	log.Printf("  Mirror Public Repos: %t\n", c.MirrorPublicRepos)
	log.Printf("  Mirror Private Repos: %t\n", c.MirrorPrivateRepos)
	log.Printf("  Mirror Forks: %t\n", c.MirrorForks)
	log.Printf("  SyncInterval: %d (seconds)\n", c.SyncInterval)
	log.Printf("  Number of Workers: %d\n", c.NumWorkers)
	log.Printf("  Dry Run: %t\n", c.DryRun)
}

func (c *Config) resolve() error {
	if c.GiteaUsername == "" {
		client, err := GiteaClient(context.Background(), *c)
		if err != nil {
			return fmt.Errorf("error creating Gitea client: %w\n", err)
		}
		username, err := GiteaGetUsername(client)
		if err != nil {
			return fmt.Errorf("error getting Gitea username: %w", err)
		}
		c.GiteaUsername = username
	}
	return nil
}

func (c *Config) validate() error {
	errors := []string{}
	if c.GithubUsername == "" {
		errors = append(errors, "GITHUB_USERNAME environment variable not set")
	}
	if c.GiteaUrl == "" {
		errors = append(errors, "GITEA_URL environment variable not set")
	}
	if c.GiteaToken == "" {
		errors = append(errors, "GITEA_TOKEN environment variable not set")
	}
	if c.MirrorPrivateRepos && c.GithubToken == nil {
		errors = append(errors, "GITHUB_TOKEN environment variable not set (required for mirroring private repos)")
	}
	if len(errors) > 0 {
		return fmt.Errorf("config validation errors: %s", strings.Join(errors, ", "))
	}
	return nil
}

func MakeConfigFromEnv() (Config, error) {
	var envErrors []error

	githubUsername, err := GetEnvStrict("GITHUB_USERNAME")
	if err != nil {
		envErrors = append(envErrors, err)
	}
	giteaUrl, err := GetEnvStrict("GITEA_URL")
	if err != nil {
		envErrors = append(envErrors, err)
	}
	giteaToken, err := GetEnvStrict("GITEA_TOKEN")
	if err != nil {
		envErrors = append(envErrors, err)
	}
	if len(envErrors) > 0 {
		return Config{}, errors.Join(envErrors...)
	}

	c := Config{
		GithubUsername:     githubUsername,
		GiteaUrl:           giteaUrl,
		GiteaToken:         giteaToken,
		GithubToken:        GetEnvOptional("GITHUB_TOKEN"),
		NumWorkers:         GetEnvInt("HUBTOTEA_NUM_WORKERS", 5),
		MirrorPublicRepos:  GetEnvBool("HUBTOTEA_MIRROR_PUBLIC_REPOS", true),
		MirrorPrivateRepos: GetEnvBool("HUBTOTEA_MIRROR_PRIVATE_REPOS", false),
		MirrorForks:        GetEnvBool("HUBTOTEA_MIRROR_FORKS", false),
		DryRun:             GetEnvBool("HUBTOTEA_DRY_RUN", false),
		SyncInterval:       GetEnvInt("HUBTOTEA_SYNC_INTERVAL", 3600),
	}
	err = c.validate()
	if err != nil {
		return Config{}, fmt.Errorf("error validating config: %w", err)
	}
	err = c.resolve()
	if err != nil {
		return Config{}, fmt.Errorf("error resolving config: %w", err)
	}
	return c, nil
}
