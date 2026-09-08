package main

import (
	"sync"
	"time"
)

type RepoSyncResult struct {
	Name   string
	Source RepositorySource
	Result MirrorResult
	Error  string
}

type RepoFailure struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type OrganizationStats struct {
	Name   string             `json:"name"`
	Result OrganizationResult `json:"result"`
	Error  string             `json:"error,omitempty"`
}

type RunStats struct {
	RunCount             int                `json:"run_count"`
	Status               string             `json:"status"`
	StartedAt            time.Time          `json:"started_at"`
	FinishedAt           *time.Time         `json:"finished_at,omitempty"`
	DurationSeconds      float64            `json:"duration_seconds,omitempty"`
	Error                string             `json:"error,omitempty"`
	TotalRead            int                `json:"total_read"`
	OwnedDiscovered      int                `json:"owned_discovered"`
	StarredDiscovered    int                `json:"starred_discovered"`
	Duplicates           int                `json:"duplicates"`
	Created              int                `json:"created"`
	Skipped              int                `json:"skipped"`
	WouldCreate          int                `json:"would_create"`
	Failed               int                `json:"failed"`
	Deferred             int                `json:"deferred"`
	StarredBacklog       int                `json:"starred_backlog"`
	StarredEnabled       bool               `json:"starred_enabled"`
	StarredOrganization  *OrganizationStats `json:"starred_organization,omitempty"`
	CreatedRepositories  []string           `json:"created_repositories"`
	WouldCreateRepos     []string           `json:"would_create_repositories"`
	DeferredRepositories []string           `json:"deferred_repositories"`
	FailedRepositories   []RepoFailure      `json:"failed_repositories"`
}

func (s *RunStats) record(result RepoSyncResult) {
	switch result.Result {
	case Created:
		s.Created++
		s.CreatedRepositories = append(s.CreatedRepositories, result.Name)
	case Skipped:
		s.Skipped++
	case WouldCreate:
		s.WouldCreate++
		s.WouldCreateRepos = append(s.WouldCreateRepos, result.Name)
	case Failed:
		s.Failed++
		s.FailedRepositories = append(s.FailedRepositories, RepoFailure{
			Name:  result.Name,
			Error: result.Error,
		})
	case Deferred:
		s.Deferred++
		s.DeferredRepositories = append(s.DeferredRepositories, result.Name)
	}
	if result.Source == StarredRepositorySource {
		switch result.Result {
		case WouldCreate, Failed, Deferred:
			s.StarredBacklog++
		}
	}
}

type StatsSnapshot struct {
	Version             string     `json:"version"`
	ServiceStartedAt    time.Time  `json:"service_started_at"`
	SyncIntervalSeconds int        `json:"sync_interval_seconds"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRun             *RunStats  `json:"last_run,omitempty"`
	CurrentRun          *RunStats  `json:"current_run,omitempty"`
}

type StatsStore struct {
	mu                  sync.RWMutex
	version             string
	serviceStartedAt    time.Time
	syncIntervalSeconds int
	nextRunAt           time.Time
	lastRun             *RunStats
	currentRun          *RunStats
}

func NewStatsStore(version string, syncIntervalSeconds int) *StatsStore {
	return &StatsStore{
		version:             version,
		serviceStartedAt:    time.Now(),
		syncIntervalSeconds: syncIntervalSeconds,
	}
}

func (s *StatsStore) StartRun(runCount int, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentRun = &RunStats{
		RunCount:  runCount,
		Status:    "running",
		StartedAt: startedAt,
	}
	s.nextRunAt = time.Time{}
}

func (s *StatsStore) FinishRun(stats RunStats, finishedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats.FinishedAt = &finishedAt
	stats.DurationSeconds = finishedAt.Sub(stats.StartedAt).Seconds()
	s.lastRun = &stats
	s.currentRun = nil
}

func (s *StatsStore) SetNextRun(nextRunAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRunAt = nextRunAt
}

func (s *StatsStore) ClearNextRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRunAt = time.Time{}
}

func (s *StatsStore) Snapshot() StatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var nextRunAt *time.Time
	if !s.nextRunAt.IsZero() {
		next := s.nextRunAt
		nextRunAt = &next
	}
	return StatsSnapshot{
		Version:             s.version,
		ServiceStartedAt:    s.serviceStartedAt,
		SyncIntervalSeconds: s.syncIntervalSeconds,
		NextRunAt:           nextRunAt,
		LastRun:             cloneRunStats(s.lastRun),
		CurrentRun:          cloneRunStats(s.currentRun),
	}
}

func cloneRunStats(stats *RunStats) *RunStats {
	if stats == nil {
		return nil
	}
	clone := *stats
	clone.CreatedRepositories = append([]string(nil), stats.CreatedRepositories...)
	clone.WouldCreateRepos = append([]string(nil), stats.WouldCreateRepos...)
	clone.DeferredRepositories = append([]string(nil), stats.DeferredRepositories...)
	clone.FailedRepositories = append([]RepoFailure(nil), stats.FailedRepositories...)
	if stats.StarredOrganization != nil {
		organization := *stats.StarredOrganization
		clone.StarredOrganization = &organization
	}
	return &clone
}
