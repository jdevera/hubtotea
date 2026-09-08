package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRecordRunLifecycle(t *testing.T) {
	metrics := NewMetrics("test", 3600)
	startedAt := time.Unix(1_700_000_000, 0)
	finishedAt := startedAt.Add(12 * time.Second)

	metrics.SetNextRun(startedAt.Add(time.Hour))
	metrics.StartRun(1, startedAt)
	assertMetricValue(t, metrics.runInProgress, 1)
	assertMetricValue(t, metrics.currentRunStartTimestamp, timestampSeconds(startedAt))
	assertMetricValue(t, metrics.nextRunTimestamp, 0)

	metrics.FinishRun(RunStats{
		Status:              "completed_with_errors",
		StartedAt:           startedAt,
		OwnedDiscovered:     5,
		StarredDiscovered:   4,
		Created:             2,
		Skipped:             3,
		WouldCreate:         4,
		Failed:              1,
		StarredBacklog:      2,
		StarredOrganization: &OrganizationStats{Result: OrganizationCreated},
	}, finishedAt)

	assertMetricValue(t, metrics.runInProgress, 0)
	assertMetricValue(t, metrics.currentRunStartTimestamp, 0)
	assertMetricValue(t, metrics.runsTotal.WithLabelValues("completed_with_errors"), 1)
	assertMetricValue(t, metrics.lastRunStatus.WithLabelValues("completed_with_errors"), 1)
	assertMetricValue(t, metrics.lastRunStatus.WithLabelValues("success"), 0)
	assertMetricValue(t, metrics.repositoryResultsTotal.WithLabelValues("created"), 2)
	assertMetricValue(t, metrics.repositoryResultsTotal.WithLabelValues("skipped"), 3)
	assertMetricValue(t, metrics.repositoryResultsTotal.WithLabelValues("would_create"), 4)
	assertMetricValue(t, metrics.repositoryResultsTotal.WithLabelValues("failed"), 1)
	assertMetricValue(t, metrics.lastRunRepositoryResults.WithLabelValues("failed"), 1)
	assertMetricValue(t, metrics.lastRunRepositoriesDiscovered.WithLabelValues("owned"), 5)
	assertMetricValue(t, metrics.lastRunRepositoriesDiscovered.WithLabelValues("starred"), 4)
	assertMetricValue(t, metrics.starredRepositoryBacklog, 2)
	assertMetricValue(t, metrics.organizationOperations.WithLabelValues("created"), 1)
	assertMetricValue(t, metrics.lastRunTimestamp, timestampSeconds(finishedAt))
}

func TestNewMetricsInstanceStartsWithResetProcessState(t *testing.T) {
	first := NewMetrics("test", 3600)
	first.FinishRun(RunStats{
		Status:    "success",
		StartedAt: time.Now().Add(-time.Second),
		Created:   3,
	}, time.Now())

	second := NewMetrics("test", 3600)
	assertMetricValue(t, second.runsTotal.WithLabelValues("success"), 0)
	assertMetricValue(t, second.repositoryResultsTotal.WithLabelValues("created"), 0)
	assertMetricValue(t, second.lastRunStatus.WithLabelValues("success"), 0)
	assertMetricValue(t, second.lastRunRepositoriesDiscovered.WithLabelValues("starred"), 0)
	assertMetricValue(t, second.starredRepositoryBacklog, 0)
	assertMetricValue(t, second.organizationOperations.WithLabelValues("created"), 0)
	assertMetricValue(t, second.lastRunTimestamp, 0)
}

func TestMetricsMapUnexpectedRunStatusToBoundedLabel(t *testing.T) {
	metrics := NewMetrics("test", 0)
	metrics.FinishRun(RunStats{Status: "unexpected"}, time.Now())

	assertMetricValue(t, metrics.runsTotal.WithLabelValues("unknown"), 1)
	assertMetricValue(t, metrics.lastRunStatus.WithLabelValues("unknown"), 1)
}

func TestMetricsPassPrometheusLint(t *testing.T) {
	metrics := NewMetrics("test", 3600)
	problems, err := testutil.GatherAndLint(metrics.registry)
	if err != nil {
		t.Fatalf("lint metrics: %v", err)
	}
	if len(problems) > 0 {
		t.Fatalf("metric lint problems: %v", problems)
	}
}

func assertMetricValue(t *testing.T, collector prometheus.Collector, want float64) {
	t.Helper()
	if got := testutil.ToFloat64(collector); got != want {
		t.Fatalf("metric value = %v, want %v", got, want)
	}
}
