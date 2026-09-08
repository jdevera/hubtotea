package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v63/github"
)

func TestSyncOnceRecordsTopLevelErrors(t *testing.T) {
	wantError := errors.New("GitHub unavailable")
	stats := syncOnce(context.Background(), Config{RunTimeout: time.Second}, func(context.Context, Config) (RunStats, error) {
		return RunStats{TotalRead: 2}, wantError
	})

	if stats.Status != "error" || stats.Error != wantError.Error() {
		t.Fatalf("error status = %+v", stats)
	}
	if stats.TotalRead != 2 {
		t.Fatalf("total read = %d, want 2", stats.TotalRead)
	}
}

func TestSyncOnceAppliesRunTimeout(t *testing.T) {
	stats := syncOnce(context.Background(), Config{RunTimeout: 10 * time.Millisecond}, func(ctx context.Context, _ Config) (RunStats, error) {
		<-ctx.Done()
		return RunStats{}, ctx.Err()
	})

	if stats.Status != "error" || !strings.Contains(stats.Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout status = %+v", stats)
	}
}

func TestRunEveryRunsOnceWhenIntervalIsZero(t *testing.T) {
	store := NewStatsStore("test", 0)
	calls := 0

	runEvery(context.Background(), 0, store, func(_ context.Context, runCount int) RunStats {
		calls++
		return RunStats{
			Status:    "success",
			TotalRead: 3,
		}
	})

	if calls != 1 {
		t.Fatalf("runs = %d, want 1", calls)
	}
	snapshot := store.Snapshot()
	if snapshot.CurrentRun != nil {
		t.Fatal("current run is still set after completion")
	}
	if snapshot.NextRunAt != nil {
		t.Fatal("next run is set for a one-shot synchronization")
	}
	if snapshot.LastRun == nil {
		t.Fatal("last run is not set")
	}
	if snapshot.LastRun.RunCount != 1 {
		t.Fatalf("run count = %d, want 1", snapshot.LastRun.RunCount)
	}
	if snapshot.LastRun.TotalRead != 3 {
		t.Fatalf("total read = %d, want 3", snapshot.LastRun.TotalRead)
	}
	if snapshot.LastRun.StartedAt.IsZero() || snapshot.LastRun.FinishedAt == nil {
		t.Fatal("run timestamps are incomplete")
	}
}

func TestRunEveryCancellationInterruptsWait(t *testing.T) {
	store := NewStatsStore("test", 3600)
	ctx, cancel := context.WithCancel(context.Background())
	runStarted := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		runEvery(ctx, time.Hour, store, func(_ context.Context, _ int) RunStats {
			close(runStarted)
			return RunStats{Status: "success"}
		})
	}()

	select {
	case <-runStarted:
	case <-time.After(time.Second):
		t.Fatal("synchronization did not start")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}

	snapshot := store.Snapshot()
	if snapshot.LastRun == nil {
		t.Fatal("completed run was not recorded")
	}
	if snapshot.NextRunAt != nil {
		t.Fatal("next run remains set after cancellation")
	}
}

func TestSyncRepoListArchivesStarredRepositoriesWithDeduplicationAndLimit(t *testing.T) {
	owned := githubRepository(1, "source", "owned")
	listRepositories := func(context.Context, Config) (GithubRepositories, error) {
		return GithubRepositories{
			Owned: []*github.Repository{owned},
			Starred: []*github.Repository{
				owned,
				githubRepository(2, "other", "existing"),
				githubRepository(3, "other", "created"),
				githubRepository(4, "other", "failed"),
				githubRepository(5, "other", "deferred"),
			},
		}, nil
	}
	organizationCalls := 0
	ensureOrganization := func(context.Context, Config) (OrganizationResult, error) {
		organizationCalls++
		return OrganizationExisting, nil
	}
	mirror := func(_ context.Context, plan RepositoryPlan, _ Config, limiter *creationLimiter) (MirrorResult, error) {
		switch plan.Repository.GetName() {
		case "existing":
			return Skipped, nil
		case "failed":
			if !limiter.reserve(plan.Source) {
				return Deferred, nil
			}
			return Failed, errors.New("migration failed")
		case "deferred":
			if !limiter.reserve(plan.Source) {
				return Deferred, nil
			}
			return Created, nil
		default:
			if !limiter.reserve(plan.Source) {
				return Deferred, nil
			}
			return Created, nil
		}
	}

	stats, err := syncRepoList(context.Background(), Config{
		GithubUsername:          "source",
		ForgejoUsername:         "forgejo-user",
		StarredOrg:              "github-stars",
		MirrorStarredRepos:      true,
		MaxStarredCreatesPerRun: 2,
		NumWorkers:              1,
	}, listRepositories, ensureOrganization, mirror)
	if err != nil {
		t.Fatalf("synchronize repositories: %v", err)
	}
	if organizationCalls != 1 {
		t.Fatalf("organization checks = %d, want 1", organizationCalls)
	}
	if stats.TotalRead != 5 || stats.OwnedDiscovered != 1 || stats.StarredDiscovered != 5 || stats.Duplicates != 1 {
		t.Fatalf("unexpected discovery stats: %+v", stats)
	}
	if stats.Created != 2 || stats.Skipped != 1 || stats.Failed != 1 || stats.Deferred != 1 || stats.StarredBacklog != 2 {
		t.Fatalf("unexpected result stats: %+v", stats)
	}
	if stats.StarredOrganization == nil || stats.StarredOrganization.Result != OrganizationExisting {
		t.Fatalf("unexpected organization stats: %+v", stats.StarredOrganization)
	}
}

func TestSyncRepoListDoesNotManageOrganizationWhenStarredMirroringIsDisabled(t *testing.T) {
	ensureOrganization := func(context.Context, Config) (OrganizationResult, error) {
		t.Fatal("legacy configuration attempted to manage a Forgejo organization")
		return OrganizationFailed, nil
	}
	listRepositories := func(context.Context, Config) (GithubRepositories, error) {
		return GithubRepositories{Owned: []*github.Repository{githubRepository(1, "source", "owned")}}, nil
	}
	mirror := func(context.Context, RepositoryPlan, Config, *creationLimiter) (MirrorResult, error) {
		return Skipped, nil
	}

	stats, err := syncRepoList(context.Background(), Config{
		ForgejoUsername: "forgejo-user",
		NumWorkers:      1,
	}, listRepositories, ensureOrganization, mirror)
	if err != nil {
		t.Fatalf("synchronize repositories: %v", err)
	}
	if stats.TotalRead != 1 || stats.Skipped != 1 || stats.StarredEnabled {
		t.Fatalf("unexpected legacy stats: %+v", stats)
	}
}
