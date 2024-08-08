package main

import (
	"context"
	"github.com/google/go-github/v63/github"
	"log"
	"sync"
	"time"
)

type SyncResult map[MirrorResult]int

// Version of the application. It will be set during the build process.
var Version = "dev"

func MirrorWorker(ctx context.Context, id int, wg *sync.WaitGroup, repos <-chan *github.Repository, stats chan<- MirrorResult, config Config) {
	defer wg.Done()
	log.Printf("[Worker %d] Starting\n", id)
	ctx = context.WithValue(ctx, "worker_id", id)
	for repo := range repos {
		log.Printf("[Worker %d] Processing repository %s\n", id, *repo.FullName)
		res, err := GiteaMirror(ctx, repo, config)
		if err != nil {
			log.Printf("[Worker %d] Error mirroring repository %s: %s\n", id, *repo.FullName, err)
		}
		stats <- res
	}
	log.Printf("[Worker %d] Done\n", id)
}

func SyncRepoList(ctx context.Context, config Config) (SyncResult, error) {
	repos, err := GetGithubRepos(ctx, config)
	if err != nil {
		return nil, err
	}

	log.Printf("Found %d repositories\n", len(repos))
	for _, repo := range repos {
		log.Printf("Repository -> name: %v, private=%v, fork=%v\n", *repo.FullName, *repo.Private, *repo.Fork)
	}

	repoChan := make(chan *github.Repository, len(repos))
	statsChan := make(chan MirrorResult, len(repos))
	var wg sync.WaitGroup

	resultsStats := make(SyncResult)

	for workerId := 0; workerId < config.NumWorkers; workerId++ {
		wg.Add(1)
		go MirrorWorker(ctx, workerId, &wg, repoChan, statsChan, config)
	}

	for _, repo := range repos {
		repoChan <- repo
	}
	close(repoChan)

	wg.Wait()

	close(statsChan)
	for mirrorResult := range statsChan {
		resultsStats[mirrorResult]++
	}

	resultsStats[Input] = len(repos)
	return resultsStats, nil

}

func runEvery(interval time.Duration, f func(int)) {
	loggingWrapper := func(runCount int) {
		startTime := time.Now()
		f(runCount)
		elapsed := time.Since(startTime)
		nextRun := interval - elapsed
		if nextRun < 0 {
			log.Printf("Operation took longer than the interval: %s\n", elapsed)
			return
		}
		log.Printf("Next run in ~%s\n", nextRun.Round(time.Second))
	}
	runCount := 1
	loggingWrapper(runCount)
	if interval <= 0 {
		return
	}

	// If the operation takes longer than the interval, the next run will start immediately
	// after the previous one finishes.
	for range time.Tick(interval) {
		runCount++
		loggingWrapper(runCount)
	}
}

func main() {
	log.SetFlags(0)
	config, err := MakeConfigFromEnv()
	if err != nil {
		log.Fatalf("HubToTea version: %s\nConfig error: %s\n", Version, err)
	}

	runEvery(time.Duration(config.SyncInterval)*time.Second,
		func(runCount int) {
			log.Println("--------------------------------------------------")
			log.Printf("HubToTea version: %s\n", Version)
			log.Printf("Run #%d\n", runCount)
			config.log()
			log.Println("--------------------------------------------------")

			resultsStats, err := SyncRepoList(context.Background(), config)
			log.Printf("--------------------------------------------------\n")
			log.Printf("Results:\n")
			if err != nil {
				log.Printf("  Error: %s\n", err.Error())
				log.Printf("--------------------------------------------------\n")
				return
			}
			log.Printf("  Total Read: %d\n", resultsStats[Input])
			log.Printf("  Created: %d\n", resultsStats[Created])
			log.Printf("  Skipped: %d\n", resultsStats[Skipped])
			log.Printf("  WouldCreate: %d\n", resultsStats[WouldCreate])
			log.Printf("  Failed: %d\n", resultsStats[Failed])
			log.Printf("--------------------------------------------------\n")
		})
}
