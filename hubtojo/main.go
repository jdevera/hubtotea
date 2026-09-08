package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// Version of the application. It will be set during the build process.
var Version = "dev"

type repositoryLister func(context.Context, Config) (GithubRepositories, error)
type repositoryMirror func(context.Context, RepositoryPlan, Config, *creationLimiter) (MirrorResult, error)
type organizationEnsurer func(context.Context, Config) (OrganizationResult, error)
type repositorySynchronizer func(context.Context, Config) (RunStats, error)

type workerIDContextKey struct{}

func MirrorWorker(ctx context.Context, id int, wg *sync.WaitGroup, repos <-chan RepositoryPlan, stats chan<- RepoSyncResult, config Config, limiter *creationLimiter, mirror repositoryMirror) {
	defer wg.Done()
	log.Printf("[Worker %d] Starting\n", id)
	ctx = context.WithValue(ctx, workerIDContextKey{}, id)
	for plan := range repos {
		name := plan.Repository.GetFullName()
		log.Printf("[Worker %d] Processing repository %s\n", id, name)
		res, err := mirror(ctx, plan, config, limiter)
		result := RepoSyncResult{
			Name:   name,
			Source: plan.Source,
			Result: res,
		}
		if err != nil {
			log.Printf("[Worker %d] Error mirroring repository %s: %s\n", id, name, err)
			result.Error = err.Error()
		}
		stats <- result
	}
	log.Printf("[Worker %d] Done\n", id)
}

func SyncRepoList(ctx context.Context, config Config) (RunStats, error) {
	return syncRepoList(ctx, config, GetGithubRepositories, EnsureStarredOrganization, ForgejoMirror)
}

func syncRepoList(ctx context.Context, config Config, listRepositories repositoryLister, ensureOrganization organizationEnsurer, mirror repositoryMirror) (RunStats, error) {
	repositories, err := listRepositories(ctx, config)
	resultsStats := RunStats{
		StarredEnabled:    config.MirrorStarredRepos,
		OwnedDiscovered:   len(repositories.Owned),
		StarredDiscovered: len(repositories.Starred),
	}
	if err != nil {
		return resultsStats, err
	}
	plans, err := PlanRepositories(config, repositories)
	if err != nil {
		return resultsStats, err
	}
	resultsStats.TotalRead = len(plans.Repositories)
	resultsStats.Duplicates = plans.Duplicates

	log.Printf("Found %d owned and %d starred repositories (%d unique)\n", plans.OwnedDiscovered, plans.StarredDiscovered, len(plans.Repositories))
	usesStarredArchive := false
	starredPlanned := 0
	for _, plan := range plans.Repositories {
		repository := plan.Repository
		log.Printf("Repository -> name: %v, source=%s, destination=%s/%s, private=%v, fork=%v\n", repository.GetFullName(), plan.Source, plan.DestinationOwner, plan.DestinationName, repository.GetPrivate(), repository.GetFork())
		if plan.Source == StarredRepositorySource {
			starredPlanned++
		}
		usesStarredArchive = usesStarredArchive || plan.UsesStarredArchive
	}

	if usesStarredArchive {
		organizationResult, ensureErr := ensureOrganization(ctx, config)
		resultsStats.StarredOrganization = &OrganizationStats{
			Name:   config.StarredOrg,
			Result: organizationResult,
		}
		if ensureErr != nil {
			resultsStats.StarredOrganization.Error = ensureErr.Error()
			resultsStats.StarredBacklog = starredPlanned
			return resultsStats, ensureErr
		}
	}

	repoChan := make(chan RepositoryPlan, len(plans.Repositories))
	statsChan := make(chan RepoSyncResult, len(plans.Repositories))
	var wg sync.WaitGroup
	limiter := newCreationLimiter(config.MaxStarredCreatesPerRun)

	for workerId := 0; workerId < config.NumWorkers; workerId++ {
		wg.Add(1)
		go MirrorWorker(ctx, workerId, &wg, repoChan, statsChan, config, limiter, mirror)
	}

	for _, plan := range plans.Repositories {
		repoChan <- plan
	}
	close(repoChan)

	wg.Wait()

	close(statsChan)
	for mirrorResult := range statsChan {
		resultsStats.record(mirrorResult)
	}

	return resultsStats, nil
}

func syncOnce(ctx context.Context, config Config, synchronize repositorySynchronizer) RunStats {
	runCtx, cancel := config.withRunTimeout(ctx)
	defer cancel()

	runStats, err := synchronize(runCtx, config)
	log.Printf("--------------------------------------------------\n")
	log.Printf("Results:\n")
	if err != nil {
		log.Printf("  Error: %s\n", err.Error())
		log.Printf("--------------------------------------------------\n")
		runStats.Status = "error"
		runStats.Error = err.Error()
		return runStats
	}
	runStats.Status = "success"
	if runStats.Failed > 0 {
		runStats.Status = "completed_with_errors"
	}
	log.Printf("  Total Read: %d\n", runStats.TotalRead)
	log.Printf("  Created: %d\n", runStats.Created)
	log.Printf("  Skipped: %d\n", runStats.Skipped)
	log.Printf("  WouldCreate: %d\n", runStats.WouldCreate)
	log.Printf("  Failed: %d\n", runStats.Failed)
	if runStats.StarredEnabled {
		log.Printf("  Starred Backlog: %d\n", runStats.StarredBacklog)
	}
	log.Printf("--------------------------------------------------\n")
	return runStats
}

type runRecorder interface {
	StartRun(int, time.Time)
	FinishRun(RunStats, time.Time)
	SetNextRun(time.Time)
	ClearNextRun()
}

type runRecorders []runRecorder

func (recorders runRecorders) StartRun(runCount int, startedAt time.Time) {
	for _, recorder := range recorders {
		recorder.StartRun(runCount, startedAt)
	}
}

func (recorders runRecorders) FinishRun(stats RunStats, finishedAt time.Time) {
	for _, recorder := range recorders {
		recorder.FinishRun(stats, finishedAt)
	}
}

func (recorders runRecorders) SetNextRun(nextRunAt time.Time) {
	for _, recorder := range recorders {
		recorder.SetNextRun(nextRunAt)
	}
}

func (recorders runRecorders) ClearNextRun() {
	for _, recorder := range recorders {
		recorder.ClearNextRun()
	}
}

func runEvery(ctx context.Context, interval time.Duration, recorder runRecorder, f func(context.Context, int) RunStats) {
	runCount := 1
	for {
		select {
		case <-ctx.Done():
			recorder.ClearNextRun()
			return
		default:
		}

		startTime := time.Now()
		recorder.StartRun(runCount, startTime)
		stats := f(ctx, runCount)
		stats.RunCount = runCount
		stats.StartedAt = startTime
		elapsed := time.Since(startTime)
		recorder.FinishRun(stats, time.Now())
		if interval <= 0 {
			recorder.ClearNextRun()
			return
		}
		nextRun := interval - elapsed
		if nextRun < 0 {
			log.Printf("Operation took longer than the interval: %s\n", elapsed)
			nextRun = 0
		}
		nextRunAt := time.Now().Add(nextRun)
		recorder.SetNextRun(nextRunAt)
		log.Printf("Next run in ~%s\n", nextRun.Round(time.Second))
		if nextRun > 0 {
			timer := time.NewTimer(nextRun)
			select {
			case <-ctx.Done():
				timer.Stop()
				recorder.ClearNextRun()
				return
			case <-timer.C:
			}
		}
		runCount++
	}
}

func main() {
	log.SetFlags(0)
	config, err := MakeConfigFromEnv()
	if err != nil {
		log.Fatalf("HubToJo version: %s\nConfig error: %s\n", Version, err)
	}

	statsStore := NewStatsStore(Version, config.SyncInterval)
	metrics := NewMetrics(Version, config.SyncInterval)
	if _, err := StartWebServer(config.WebAddr, statsStore, metrics); err != nil {
		log.Fatalf("Web server error: %s\n", err)
	}

	runEvery(context.Background(), time.Duration(config.SyncInterval)*time.Second,
		runRecorders{statsStore, metrics},
		func(ctx context.Context, runCount int) RunStats {
			log.Println("--------------------------------------------------")
			log.Printf("HubToJo version: %s\n", Version)
			log.Printf("Run #%d\n", runCount)
			config.log()
			log.Println("--------------------------------------------------")

			return syncOnce(ctx, config, SyncRepoList)
		})
}
