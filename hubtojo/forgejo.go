package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
)

type MirrorResult int

const (
	Created MirrorResult = iota
	WouldCreate
	Skipped
	Failed
	Deferred
)

type OrganizationResult string

const (
	OrganizationCreated     OrganizationResult = "created"
	OrganizationWouldCreate OrganizationResult = "would_create"
	OrganizationExisting    OrganizationResult = "existing"
	OrganizationFailed      OrganizationResult = "failed"
)

func ForgejoGetUsername(client *forgejo.Client) (string, error) {
	user, _, err := client.GetMyUserInfo()
	if err != nil {
		return "", err
	}
	return user.UserName, nil
}

func ForgejoClient(ctx context.Context, config Config) (*forgejo.Client, error) {
	return forgejo.NewClient(config.ForgejoUrl, forgejo.SetToken(config.ForgejoToken), forgejo.SetContext(ctx))
}

func EnsureStarredOrganization(ctx context.Context, config Config) (OrganizationResult, error) {
	client, err := ForgejoClient(ctx, config)
	if err != nil {
		return OrganizationFailed, err
	}
	_, resp, err := client.GetOrg(config.StarredOrg)
	if err == nil {
		permissions, _, permissionsErr := client.GetOrgPermissions(config.StarredOrg, config.ForgejoUsername)
		if permissionsErr != nil {
			return OrganizationFailed, fmt.Errorf("check permissions for Forgejo organization %q: %w", config.StarredOrg, permissionsErr)
		}
		if !permissions.IsOwner {
			return OrganizationFailed, fmt.Errorf("Forgejo user %q is not an owner of organization %q", config.ForgejoUsername, config.StarredOrg)
		}
		log.Printf("Using existing Forgejo organization %s for starred repositories\n", config.StarredOrg)
		return OrganizationExisting, nil
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		return OrganizationFailed, fmt.Errorf("check Forgejo organization %q: %w", config.StarredOrg, err)
	}
	if config.DryRun {
		log.Printf("[DRY-RUN] Would create Forgejo organization %s for starred repositories\n", config.StarredOrg)
		return OrganizationWouldCreate, nil
	}

	_, _, err = client.CreateOrg(forgejo.CreateOrgOption{
		Name:        config.StarredOrg,
		FullName:    "GitHub starred repository archive",
		Description: "GitHub starred repositories mirrored by HubToJo",
		Visibility:  forgejo.VisibleTypePublic,
	})
	if err != nil {
		return OrganizationFailed, fmt.Errorf("create Forgejo organization %q: %w", config.StarredOrg, err)
	}
	log.Printf("Created Forgejo organization %s for starred repositories\n", config.StarredOrg)
	return OrganizationCreated, nil
}

// ForgejoMirror creates a Forgejo mirror at the planned destination.
func ForgejoMirror(ctx context.Context, plan RepositoryPlan, config Config, limiter *creationLimiter) (MirrorResult, error) {
	prefix := ""
	if workerId := ctx.Value(workerIDContextKey{}); workerId != nil {
		prefix = fmt.Sprintf("[Worker %d] ", workerId)
	}
	githubRepo := plan.Repository
	client, err := forgejo.NewClient(config.ForgejoUrl,
		forgejo.SetToken(config.ForgejoToken),
		forgejo.SetContext(ctx),
	)
	if err != nil {
		return Failed, err
	}
	forgejoRepo, resp, err := client.GetRepo(plan.DestinationOwner, plan.DestinationName)
	if err == nil {
		log.Printf("%sSkipping repository %s. It already exists on Forgejo\n", prefix, forgejoRepo.FullName)
		return Skipped, nil
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		return Failed, fmt.Errorf("check Forgejo repository %s/%s: %w", plan.DestinationOwner, plan.DestinationName, err)
	}
	if !limiter.reserve(plan.Source) {
		log.Printf("%sDeferring repository %s because the starred creation limit was reached\n", prefix, githubRepo.GetFullName())
		return Deferred, nil
	}
	if config.DryRun {
		log.Printf("%s[DRY-RUN] Would create repository %s as %s/%s on Forgejo\n", prefix, githubRepo.GetFullName(), plan.DestinationOwner, plan.DestinationName)
		return WouldCreate, nil
	}
	log.Printf("%sCreating repository %s as %s/%s on Forgejo\n", prefix, githubRepo.GetFullName(), plan.DestinationOwner, plan.DestinationName)

	githubAuth := ""
	if config.GithubToken != nil {
		githubAuth = *config.GithubToken
	}
	option := forgejo.MigrateRepoOption{
		AuthToken: githubAuth,
		CloneAddr: githubRepo.GetCloneURL(),
		RepoName:  plan.DestinationName,
		RepoOwner: plan.DestinationOwner,
		Private:   githubRepo.GetPrivate(),
		Mirror:    true,
	}
	if githubRepo.Description != nil {
		option.Description = *githubRepo.Description
	}
	_, _, err = client.MigrateRepo(option)
	if err != nil {
		return Failed, err
	}
	log.Printf("%sRepository %s created on Forgejo as %s/%s\n", prefix, githubRepo.GetFullName(), plan.DestinationOwner, plan.DestinationName)
	return Created, nil
}
