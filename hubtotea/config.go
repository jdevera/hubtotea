package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
		log.Printf("  Github Token: %s\n", *c.GithubToken) // Dereference pointer to print value
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
			log.Fatalf("Error creating Gitea client: %s\n", err)
		}
		username, err := GiteaGetUsername(client)
		if err != nil {
			return fmt.Errorf("error getting Gitea username: %s", err)
		}
		c.GiteaUsername = username
	}
	return nil
}

func (c *Config) validate() {
	err := false
	if c.GithubUsername == "" {
		log.Println("Error: GITHUB_USERNAME environment variable not set")
		err = true
	}
	if c.GiteaUrl == "" {
		log.Println("Error: GITEA_URL environment variable not set")
		err = true
	}
	if c.GiteaToken == "" {
		log.Println("Error: GITEA_TOKEN environment variable not set")
		err = true
	}
	if c.MirrorPrivateRepos && c.GithubToken == nil {
		log.Println("Error: GITHUB_TOKEN environment variable not set (required for mirroring private repos)")
		err = true
	}
	if err {
		os.Exit(1)
	}
}

func MakeConfigFromEnv() (Config, error) {
	c := Config{
		GithubUsername:     GetEnvStrict("GITHUB_USERNAME"),
		GiteaUrl:           GetEnvStrict("GITEA_URL"),
		GiteaToken:         GetEnvStrict("GITEA_TOKEN"),
		GithubToken:        GetEnvOptional("GITHUB_TOKEN"),
		NumWorkers:         GetEnvInt("HUBTOTEA_NUM_WORKERS", 5),
		MirrorPublicRepos:  GetEnvBool("HUBTOTEA_MIRROR_PUBLIC_REPOS", true),
		MirrorPrivateRepos: GetEnvBool("HUBTOTEA_MIRROR_PRIVATE_REPOS", false),
		MirrorForks:        GetEnvBool("HUBTOTEA_MIRROR_FORKS", false),
		DryRun:             GetEnvBool("HUBTOTEA_DRY_RUN", false),
		SyncInterval:       GetEnvInt("HUBTOTEA_SYNC_INTERVAL", 3600),
	}
	c.validate()
	err := c.resolve()
	if err != nil {
		return c, err
	}
	return c, nil
}
