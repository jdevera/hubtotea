package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type Config struct {
	GithubUsername          string
	ForgejoUrl              string
	ForgejoToken            string
	ForgejoUsername         string
	GithubToken             *string
	NumWorkers              int
	MirrorPublicRepos       bool
	MirrorPrivateRepos      bool
	MirrorForks             bool
	MirrorStarredRepos      bool
	StarredOrg              string
	MaxStarredCreatesPerRun int
	DryRun                  bool
	SyncInterval            int
	RunTimeout              time.Duration
	WebAddr                 string
}

func (c *Config) log() {
	log.Printf("Configuration:\n")
	log.Printf("  Github Username: %s\n", c.GithubUsername)
	log.Printf("  Forgejo URL: %s\n", c.ForgejoUrl)
	log.Printf("  Forgejo Token: ****\n")
	log.Printf("  Forgejo Username: %s\n", c.ForgejoUsername)
	if c.GithubToken != nil {
		log.Printf("  Github Token: ****\n") // Dereference pointer to print value
	} else {
		log.Printf("  Github Token: Not provided\n")
	}
	log.Printf("  Mirror Public Repos: %t\n", c.MirrorPublicRepos)
	log.Printf("  Mirror Private Repos: %t\n", c.MirrorPrivateRepos)
	log.Printf("  Mirror Forks: %t\n", c.MirrorForks)
	log.Printf("  Mirror Starred Repos: %t\n", c.MirrorStarredRepos)
	if c.MirrorStarredRepos {
		log.Printf("  Starred Repository Organization: %s\n", c.StarredOrg)
		log.Printf("  Maximum Starred Creates Per Run: %d\n", c.MaxStarredCreatesPerRun)
	}
	log.Printf("  SyncInterval: %d (seconds)\n", c.SyncInterval)
	log.Printf("  Run Timeout: %s\n", c.RunTimeout)
	log.Printf("  Web Address: %s\n", c.WebAddr)
	log.Printf("  Number of Workers: %d\n", c.NumWorkers)
	log.Printf("  Dry Run: %t\n", c.DryRun)
}

func (c *Config) resolve() error {
	if c.ForgejoUsername == "" {
		ctx, cancel := c.withRunTimeout(context.Background())
		defer cancel()
		client, err := ForgejoClient(ctx, *c)
		if err != nil {
			return fmt.Errorf("error creating Forgejo client: %w", err)
		}
		username, err := ForgejoGetUsername(client)
		if err != nil {
			return fmt.Errorf("error getting Forgejo username: %w", err)
		}
		c.ForgejoUsername = username
	}
	return nil
}

func (c *Config) validate() error {
	errors := []string{}
	if c.GithubUsername == "" {
		errors = append(errors, "GITHUB_USERNAME environment variable not set")
	}
	if c.ForgejoUrl == "" {
		errors = append(errors, "FORGEJO_URL environment variable not set")
	}
	if c.ForgejoToken == "" {
		errors = append(errors, "FORGEJO_TOKEN environment variable not set")
	}
	if (c.MirrorPrivateRepos || c.MirrorStarredRepos) && (c.GithubToken == nil || strings.TrimSpace(*c.GithubToken) == "") {
		errors = append(errors, "GITHUB_TOKEN environment variable not set (required for mirroring private or starred repos)")
	}
	if c.NumWorkers <= 0 {
		errors = append(errors, "HUBTOJO_NUM_WORKERS must be greater than 0")
	}
	if c.SyncInterval < 0 {
		errors = append(errors, "HUBTOJO_SYNC_INTERVAL must be greater than or equal to 0")
	}
	if c.RunTimeout <= 0 {
		errors = append(errors, "HUBTOJO_RUN_TIMEOUT must be greater than 0")
	}
	if c.MirrorStarredRepos && strings.TrimSpace(c.StarredOrg) == "" {
		errors = append(errors, "HUBTOJO_STARRED_ORG must not be empty when mirroring starred repos")
	}
	if c.MaxStarredCreatesPerRun < 0 {
		errors = append(errors, "HUBTOJO_MAX_STARRED_CREATES_PER_RUN must be greater than or equal to 0")
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
	forgejoUrl, err := GetEnvStrict("FORGEJO_URL")
	if err != nil {
		envErrors = append(envErrors, err)
	}
	forgejoToken, err := GetEnvStrict("FORGEJO_TOKEN")
	if err != nil {
		envErrors = append(envErrors, err)
	}
	numWorkers, err := GetEnvInt("HUBTOJO_NUM_WORKERS", 5)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	mirrorPublicRepos, err := GetEnvBool("HUBTOJO_MIRROR_PUBLIC_REPOS", true)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	mirrorPrivateRepos, err := GetEnvBool("HUBTOJO_MIRROR_PRIVATE_REPOS", false)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	mirrorForks, err := GetEnvBool("HUBTOJO_MIRROR_FORKS", false)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	mirrorStarredRepos, err := GetEnvBool("HUBTOJO_MIRROR_STARRED_REPOS", false)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	maxStarredCreates, err := GetEnvInt("HUBTOJO_MAX_STARRED_CREATES_PER_RUN", 25)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	dryRun, err := GetEnvBool("HUBTOJO_DRY_RUN", false)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	syncInterval, err := GetEnvInt("HUBTOJO_SYNC_INTERVAL", 3600)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	runTimeout, err := GetEnvInt("HUBTOJO_RUN_TIMEOUT", 3600)
	if err != nil {
		envErrors = append(envErrors, err)
	}
	if len(envErrors) > 0 {
		return Config{}, errors.Join(envErrors...)
	}

	c := Config{
		GithubUsername:          githubUsername,
		ForgejoUrl:              forgejoUrl,
		ForgejoToken:            forgejoToken,
		GithubToken:             GetEnvOptional("GITHUB_TOKEN"),
		NumWorkers:              numWorkers,
		MirrorPublicRepos:       mirrorPublicRepos,
		MirrorPrivateRepos:      mirrorPrivateRepos,
		MirrorForks:             mirrorForks,
		MirrorStarredRepos:      mirrorStarredRepos,
		StarredOrg:              strings.TrimSpace(GetEnvString("HUBTOJO_STARRED_ORG", "github-stars")),
		MaxStarredCreatesPerRun: maxStarredCreates,
		DryRun:                  dryRun,
		SyncInterval:            syncInterval,
		RunTimeout:              time.Duration(runTimeout) * time.Second,
		WebAddr:                 GetEnvString("HUBTOJO_WEB_ADDR", ":8080"),
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

func (c Config) withRunTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.RunTimeout)
}
