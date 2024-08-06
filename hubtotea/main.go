package main

import (
	"context"
	"github.com/google/go-github/v63/github"
	"log"
	"sync"
	"time"
)

func MirrorWorker(ctx context.Context, id int, wg *sync.WaitGroup, repos <-chan *github.Repository, config Config) {
	defer wg.Done()
	log.Printf("[Worker %d] Starting\n", id)
	for repo := range repos {
		if config.DryRun {
			log.Printf("[Worker %d] [DRY-RUN] Would create repository %s on Gitea\n", id, *repo.FullName)
			continue
		}
		log.Printf("[Worker %d] Mirroring repository %s on Gitea\n", id, *repo.FullName)
		err := GiteaMirror(ctx, repo, config)
		if err != nil {
			log.Printf("[Worker %d] Error mirroring repository %s: %s\n", id, *repo.FullName, err)
		}
	}
	log.Printf("[Worker %d] Done\n", id)
}

func SyncRepoList(ctx context.Context, config Config) error {
	repos, err := GetGithubRepos(ctx, config)
	if err != nil {
		return err
	}

	log.Printf("Found %d repositories\n", len(repos))
	for _, repo := range repos {
		log.Printf("Repository -> name: %v, private=%v, fork=%v\n", *repo.FullName, *repo.Private, *repo.Fork)
	}

	repoChan := make(chan *github.Repository, len(repos))
	var wg sync.WaitGroup

	for workerId := 0; workerId < config.NumWorkers; workerId++ {
		wg.Add(1)
		go MirrorWorker(ctx, workerId, &wg, repoChan, config)
	}

	for _, repo := range repos {
		repoChan <- repo
	}
	close(repoChan)

	wg.Wait()

	return nil
}

func main() {
	log.SetFlags(0)
	config, err := MakeConfigFromEnv()
	if err != nil {
		log.Fatalf("Config error: %s\n", err)
	}

	isFirstRun := true
	runCount := int64(0)

	for {
		runCount++
		if !isFirstRun {
			if config.SyncInterval <= 0 {
				break
			}
			log.Printf("Waiting %d seconds before next run\n", config.SyncInterval)
			time.Sleep(time.Duration(config.SyncInterval) * time.Second)
		}
		isFirstRun = false
		log.Println("--------------------------------------------------")
		log.Printf("Run #%d\n", runCount)
		config.log()
		log.Println("--------------------------------------------------")

		err := SyncRepoList(context.Background(), config)
		if err != nil {
			log.Printf("Error: %s\n", err)
			continue
		}

	}
}
