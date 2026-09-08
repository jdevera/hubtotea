package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/google/go-github/v63/github"
)

const forgejoRepositoryNameLimit = 100

type RepositorySource string

const (
	OwnedRepositorySource   RepositorySource = "owned"
	StarredRepositorySource RepositorySource = "starred"
)

type RepositoryPlan struct {
	Repository         *github.Repository
	Source             RepositorySource
	DestinationOwner   string
	DestinationName    string
	UsesStarredArchive bool
}

type RepositoryPlans struct {
	Repositories      []RepositoryPlan
	OwnedDiscovered   int
	StarredDiscovered int
	Duplicates        int
}

func PlanRepositories(config Config, repositories GithubRepositories) (RepositoryPlans, error) {
	plans := RepositoryPlans{
		OwnedDiscovered:   len(repositories.Owned),
		StarredDiscovered: len(repositories.Starred),
	}
	seen := make(map[string]struct{}, len(repositories.Owned)+len(repositories.Starred))

	for _, repository := range repositories.Owned {
		if repository == nil {
			continue
		}
		key := githubRepositoryKey(repository)
		if _, exists := seen[key]; exists {
			plans.Duplicates++
			continue
		}
		seen[key] = struct{}{}
		plans.Repositories = append(plans.Repositories, RepositoryPlan{
			Repository:       repository,
			Source:           OwnedRepositorySource,
			DestinationOwner: config.ForgejoUsername,
			DestinationName:  repository.GetName(),
		})
	}

	for _, repository := range repositories.Starred {
		if repository == nil {
			continue
		}
		key := githubRepositoryKey(repository)
		if _, exists := seen[key]; exists {
			plans.Duplicates++
			continue
		}
		seen[key] = struct{}{}

		owner, name, err := githubRepositoryCoordinates(repository)
		if err != nil {
			return RepositoryPlans{}, err
		}
		plan := RepositoryPlan{
			Repository: repository,
			Source:     StarredRepositorySource,
		}
		if strings.EqualFold(owner, config.GithubUsername) {
			plan.DestinationOwner = config.ForgejoUsername
			plan.DestinationName = name
		} else {
			plan.DestinationOwner = config.StarredOrg
			plan.DestinationName, err = starredRepositoryName(owner, name, repository.GetID())
			if err != nil {
				return RepositoryPlans{}, err
			}
			plan.UsesStarredArchive = true
		}
		plans.Repositories = append(plans.Repositories, plan)
	}

	return plans, nil
}

func githubRepositoryKey(repository *github.Repository) string {
	if repository.GetID() != 0 {
		return "id:" + strconv.FormatInt(repository.GetID(), 10)
	}
	return "name:" + strings.ToLower(repository.GetFullName())
}

func githubRepositoryCoordinates(repository *github.Repository) (string, string, error) {
	owner := ""
	if repository.Owner != nil {
		owner = repository.Owner.GetLogin()
	}
	name := repository.GetName()
	if owner == "" || name == "" {
		fullNameOwner, fullNameRepo, found := strings.Cut(repository.GetFullName(), "/")
		if found {
			if owner == "" {
				owner = fullNameOwner
			}
			if name == "" {
				name = fullNameRepo
			}
		}
	}
	if owner == "" || name == "" {
		return "", "", fmt.Errorf("GitHub repository %q has no owner or name", repository.GetFullName())
	}
	return owner, name, nil
}

func starredRepositoryName(owner, repository string, githubID int64) (string, error) {
	name := owner + "__" + repository
	if len(name) <= forgejoRepositoryNameLimit {
		return name, nil
	}
	if githubID == 0 {
		return "", fmt.Errorf("GitHub repository %s/%s has no ID for a collision-safe truncated name", owner, repository)
	}
	suffix := "--gh" + strconv.FormatInt(githubID, 10)
	repositoryLimit := forgejoRepositoryNameLimit - len(owner) - len("__") - len(suffix)
	if repositoryLimit <= 0 {
		return "", fmt.Errorf("GitHub repository owner %q is too long for a Forgejo repository name", owner)
	}
	return owner + "__" + repository[:repositoryLimit] + suffix, nil
}

type creationLimiter struct {
	maximum  int64
	reserved atomic.Int64
}

func newCreationLimiter(maximum int) *creationLimiter {
	return &creationLimiter{maximum: int64(maximum)}
}

func (l *creationLimiter) reserve(source RepositorySource) bool {
	if source != StarredRepositorySource || l == nil || l.maximum == 0 {
		return true
	}
	for {
		reserved := l.reserved.Load()
		if reserved >= l.maximum {
			return false
		}
		if l.reserved.CompareAndSwap(reserved, reserved+1) {
			return true
		}
	}
}
