package main

import (
	"slices"
	"testing"
	"time"
)

func TestRunStatsRecordsRepositoryOutcomes(t *testing.T) {
	var stats RunStats
	results := []RepoSyncResult{
		{Name: "source/created", Result: Created},
		{Name: "source/skipped", Result: Skipped},
		{Name: "source/dry-run", Source: StarredRepositorySource, Result: WouldCreate},
		{Name: "source/failed", Result: Failed, Error: "migration failed"},
		{Name: "source/deferred", Source: StarredRepositorySource, Result: Deferred},
	}
	for _, result := range results {
		stats.record(result)
	}

	if stats.Created != 1 || stats.Skipped != 1 || stats.WouldCreate != 1 || stats.Failed != 1 || stats.Deferred != 1 {
		t.Fatalf("unexpected repository counts: %+v", stats)
	}
	if stats.StarredBacklog != 2 {
		t.Fatalf("starred backlog = %d, want 2", stats.StarredBacklog)
	}
	if !slices.Equal(stats.CreatedRepositories, []string{"source/created"}) {
		t.Fatalf("created repositories = %v", stats.CreatedRepositories)
	}
	if !slices.Equal(stats.WouldCreateRepos, []string{"source/dry-run"}) {
		t.Fatalf("dry-run repositories = %v", stats.WouldCreateRepos)
	}
	if !slices.Equal(stats.DeferredRepositories, []string{"source/deferred"}) {
		t.Fatalf("deferred repositories = %v", stats.DeferredRepositories)
	}
	wantFailures := []RepoFailure{{Name: "source/failed", Error: "migration failed"}}
	if !slices.Equal(stats.FailedRepositories, wantFailures) {
		t.Fatalf("failed repositories = %v", stats.FailedRepositories)
	}
}

func TestStatsSnapshotsDoNotExposeStoredSlices(t *testing.T) {
	store := NewStatsStore("test", 60)
	startedAt := time.Now()
	finishedAt := startedAt.Add(time.Second)
	store.FinishRun(RunStats{
		StartedAt:            startedAt,
		CreatedRepositories:  []string{"source/created"},
		WouldCreateRepos:     []string{"source/dry-run"},
		DeferredRepositories: []string{"source/deferred"},
		FailedRepositories:   []RepoFailure{{Name: "source/failed"}},
		StarredOrganization:  &OrganizationStats{Name: "github-stars", Result: OrganizationCreated},
	}, finishedAt)

	snapshot := store.Snapshot()
	snapshot.LastRun.CreatedRepositories[0] = "changed"
	snapshot.LastRun.WouldCreateRepos[0] = "changed"
	snapshot.LastRun.DeferredRepositories[0] = "changed"
	snapshot.LastRun.FailedRepositories[0].Name = "changed"
	snapshot.LastRun.StarredOrganization.Name = "changed"

	stored := store.Snapshot().LastRun
	if stored.CreatedRepositories[0] != "source/created" {
		t.Fatalf("stored created repositories changed: %v", stored.CreatedRepositories)
	}
	if stored.WouldCreateRepos[0] != "source/dry-run" {
		t.Fatalf("stored dry-run repositories changed: %v", stored.WouldCreateRepos)
	}
	if stored.DeferredRepositories[0] != "source/deferred" {
		t.Fatalf("stored deferred repositories changed: %v", stored.DeferredRepositories)
	}
	if stored.FailedRepositories[0].Name != "source/failed" {
		t.Fatalf("stored failures changed: %v", stored.FailedRepositories)
	}
	if stored.StarredOrganization.Name != "github-stars" {
		t.Fatalf("stored organization changed: %+v", stored.StarredOrganization)
	}
}
