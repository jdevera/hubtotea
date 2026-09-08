package main

import (
	"strings"
	"testing"

	"github.com/google/go-github/v63/github"
)

func TestPlanRepositoriesKeepsOwnedDestinationsAndArchivesThirdPartyStars(t *testing.T) {
	owned := githubRepository(1, "SourceUser", "owned")
	selfStar := githubRepository(2, "sourceuser", "self-star")
	thirdPartyStar := githubRepository(3, "other-owner", "project")

	plans, err := PlanRepositories(Config{
		GithubUsername:  "SourceUser",
		ForgejoUsername: "forgejo-user",
		StarredOrg:      "github-stars",
	}, GithubRepositories{
		Owned:   []*github.Repository{owned},
		Starred: []*github.Repository{githubRepository(1, "renamed-owner", "renamed-repository"), selfStar, thirdPartyStar},
	})
	if err != nil {
		t.Fatalf("plan repositories: %v", err)
	}
	if plans.OwnedDiscovered != 1 || plans.StarredDiscovered != 3 || plans.Duplicates != 1 {
		t.Fatalf("unexpected discovery counts: %+v", plans)
	}
	if len(plans.Repositories) != 3 {
		t.Fatalf("planned repositories = %d, want 3", len(plans.Repositories))
	}

	assertRepositoryDestination(t, plans.Repositories[0], OwnedRepositorySource, "forgejo-user", "owned", false)
	assertRepositoryDestination(t, plans.Repositories[1], StarredRepositorySource, "forgejo-user", "self-star", false)
	assertRepositoryDestination(t, plans.Repositories[2], StarredRepositorySource, "github-stars", "other-owner__project", true)
}

func TestStarredRepositoryNameTruncatesWithStableGithubID(t *testing.T) {
	repository := strings.Repeat("r", 100)
	first, err := starredRepositoryName("owner", repository, 123456789)
	if err != nil {
		t.Fatalf("build first repository name: %v", err)
	}
	second, err := starredRepositoryName("owner", repository, 987654321)
	if err != nil {
		t.Fatalf("build second repository name: %v", err)
	}
	if len(first) != forgejoRepositoryNameLimit {
		t.Fatalf("repository name length = %d, want %d", len(first), forgejoRepositoryNameLimit)
	}
	if !strings.HasPrefix(first, "owner__") || !strings.HasSuffix(first, "--gh123456789") {
		t.Fatalf("unexpected truncated name %q", first)
	}
	if first == second {
		t.Fatalf("different GitHub IDs produced the same name %q", first)
	}
}

func TestCreationLimiterOnlyLimitsStarredRepositories(t *testing.T) {
	limiter := newCreationLimiter(2)
	if !limiter.reserve(StarredRepositorySource) || !limiter.reserve(StarredRepositorySource) {
		t.Fatal("first two starred reservations should be allowed")
	}
	if limiter.reserve(StarredRepositorySource) {
		t.Fatal("third starred reservation should be deferred")
	}
	if !limiter.reserve(OwnedRepositorySource) {
		t.Fatal("owned repository should not be limited")
	}
	if !newCreationLimiter(0).reserve(StarredRepositorySource) {
		t.Fatal("zero limit should allow unlimited starred creates")
	}
}

func githubRepository(id int64, owner, name string) *github.Repository {
	return &github.Repository{
		ID:       github.Int64(id),
		Name:     github.String(name),
		FullName: github.String(owner + "/" + name),
		Owner:    &github.User{Login: github.String(owner)},
		CloneURL: github.String("https://github.test/" + owner + "/" + name + ".git"),
	}
}

func assertRepositoryDestination(t *testing.T, plan RepositoryPlan, source RepositorySource, owner, name string, usesArchive bool) {
	t.Helper()
	if plan.Source != source || plan.DestinationOwner != owner || plan.DestinationName != name || plan.UsesStarredArchive != usesArchive {
		t.Fatalf("unexpected repository plan: %+v", plan)
	}
}
