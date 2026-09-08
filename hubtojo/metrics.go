package main

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var runMetricStatuses = []string{"success", "completed_with_errors", "error", "unknown"}

var repositoryMetricResults = []string{"created", "skipped", "would_create", "failed"}

var organizationMetricResults = []string{"created", "existing", "failed"}

type Metrics struct {
	registry                      *prometheus.Registry
	runInProgress                 prometheus.Gauge
	currentRunStartTimestamp      prometheus.Gauge
	runsTotal                     *prometheus.CounterVec
	repositoryResultsTotal        *prometheus.CounterVec
	lastRunStatus                 *prometheus.GaugeVec
	lastRunRepositoryResults      *prometheus.GaugeVec
	lastRunRepositoriesDiscovered *prometheus.GaugeVec
	starredRepositoryBacklog      prometheus.Gauge
	organizationOperations        *prometheus.CounterVec
	lastRunTimestamp              prometheus.Gauge
	nextRunTimestamp              prometheus.Gauge
	runDuration                   prometheus.Histogram
}

func NewMetrics(version string, syncIntervalSeconds int) *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		registry: registry,
		runInProgress: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "run_in_progress",
			Help:      "Whether a synchronization run is currently in progress.",
		}),
		currentRunStartTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "current_run_start_timestamp_seconds",
			Help:      "Unix timestamp when the current synchronization run started, or zero when idle.",
		}),
		runsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "hubtojo",
			Name:      "runs_total",
			Help:      "Total number of completed synchronization runs by status.",
		}, []string{"status"}),
		repositoryResultsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "hubtojo",
			Name:      "repository_results_total",
			Help:      "Total number of repository synchronization results by outcome.",
		}, []string{"result"}),
		lastRunStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "last_run_status",
			Help:      "Status of the last completed synchronization run as a one-hot gauge.",
		}, []string{"status"}),
		lastRunRepositoryResults: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "last_run_repository_results",
			Help:      "Repository result counts from the last completed synchronization run.",
		}, []string{"result"}),
		lastRunRepositoriesDiscovered: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "last_run_repositories_discovered",
			Help:      "Repositories discovered during the last completed synchronization run by source.",
		}, []string{"source"}),
		starredRepositoryBacklog: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "starred_repository_backlog",
			Help:      "Starred repositories discovered but not confirmed as mirrored during the last completed run.",
		}),
		organizationOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "hubtojo",
			Name:      "organization_operations_total",
			Help:      "Total number of Forgejo archive organization checks by outcome.",
		}, []string{"result"}),
		lastRunTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "last_run_timestamp_seconds",
			Help:      "Unix timestamp when the last synchronization run finished, or zero before the first run.",
		}),
		nextRunTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hubtojo",
			Name:      "next_run_timestamp_seconds",
			Help:      "Unix timestamp when the next synchronization run is scheduled, or zero when none is scheduled.",
		}),
		runDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "hubtojo",
			Name:      "run_duration_seconds",
			Help:      "Duration of completed synchronization runs.",
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 7200},
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hubtojo",
		Name:      "build_info",
		Help:      "Build information for this HubToJo process.",
	}, []string{"version"})
	buildInfo.WithLabelValues(version).Set(1)

	syncInterval := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "hubtojo",
		Name:      "sync_interval_seconds",
		Help:      "Configured interval between synchronization run starts in seconds.",
	})
	syncInterval.Set(float64(syncIntervalSeconds))

	registry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		buildInfo,
		syncInterval,
		metrics.runInProgress,
		metrics.currentRunStartTimestamp,
		metrics.runsTotal,
		metrics.repositoryResultsTotal,
		metrics.lastRunStatus,
		metrics.lastRunRepositoryResults,
		metrics.lastRunRepositoriesDiscovered,
		metrics.starredRepositoryBacklog,
		metrics.organizationOperations,
		metrics.lastRunTimestamp,
		metrics.nextRunTimestamp,
		metrics.runDuration,
	)

	for _, status := range runMetricStatuses {
		metrics.runsTotal.WithLabelValues(status).Add(0)
		metrics.lastRunStatus.WithLabelValues(status).Set(0)
	}
	for _, result := range repositoryMetricResults {
		metrics.repositoryResultsTotal.WithLabelValues(result).Add(0)
		metrics.lastRunRepositoryResults.WithLabelValues(result).Set(0)
	}
	for _, source := range []RepositorySource{OwnedRepositorySource, StarredRepositorySource} {
		metrics.lastRunRepositoriesDiscovered.WithLabelValues(string(source)).Set(0)
	}
	for _, result := range organizationMetricResults {
		metrics.organizationOperations.WithLabelValues(result).Add(0)
	}

	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *Metrics) StartRun(_ int, startedAt time.Time) {
	m.runInProgress.Set(1)
	m.currentRunStartTimestamp.Set(timestampSeconds(startedAt))
	m.nextRunTimestamp.Set(0)
}

func (m *Metrics) FinishRun(stats RunStats, finishedAt time.Time) {
	status := metricRunStatus(stats.Status)
	m.runInProgress.Set(0)
	m.currentRunStartTimestamp.Set(0)
	m.runsTotal.WithLabelValues(status).Inc()
	for _, candidate := range runMetricStatuses {
		value := 0.0
		if candidate == status {
			value = 1
		}
		m.lastRunStatus.WithLabelValues(candidate).Set(value)
	}

	results := map[string]int{
		"created":      stats.Created,
		"skipped":      stats.Skipped,
		"would_create": stats.WouldCreate,
		"failed":       stats.Failed,
	}
	for _, result := range repositoryMetricResults {
		count := float64(results[result])
		m.repositoryResultsTotal.WithLabelValues(result).Add(count)
		m.lastRunRepositoryResults.WithLabelValues(result).Set(count)
	}
	m.lastRunRepositoriesDiscovered.WithLabelValues(string(OwnedRepositorySource)).Set(float64(stats.OwnedDiscovered))
	m.lastRunRepositoriesDiscovered.WithLabelValues(string(StarredRepositorySource)).Set(float64(stats.StarredDiscovered))
	m.starredRepositoryBacklog.Set(float64(stats.StarredBacklog))
	if stats.StarredOrganization != nil {
		result := string(stats.StarredOrganization.Result)
		for _, candidate := range organizationMetricResults {
			if result == candidate {
				m.organizationOperations.WithLabelValues(result).Inc()
				break
			}
		}
	}

	m.lastRunTimestamp.Set(timestampSeconds(finishedAt))
	if !stats.StartedAt.IsZero() {
		duration := finishedAt.Sub(stats.StartedAt).Seconds()
		if duration >= 0 {
			m.runDuration.Observe(duration)
		}
	}
}

func (m *Metrics) SetNextRun(nextRunAt time.Time) {
	m.nextRunTimestamp.Set(timestampSeconds(nextRunAt))
}

func (m *Metrics) ClearNextRun() {
	m.nextRunTimestamp.Set(0)
}

func metricRunStatus(status string) string {
	switch status {
	case "success", "completed_with_errors", "error":
		return status
	default:
		return "unknown"
	}
}

func timestampSeconds(value time.Time) float64 {
	return float64(value.Unix()) + float64(value.Nanosecond())/float64(time.Second)
}
