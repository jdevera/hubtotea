<p align="center">
  <img src="hubtojo/static/hubtojo-icon.png" width="180" alt="HubToJo octopus forging a repository mirror">
</p>

# HubToJo 🐙 → ⚒️: Mirror GitHub repositories to Forgejo

This program creates Forgejo mirrors of repositories owned or starred by a
GitHub user.

Best run with Docker. Forgejo 11.0.10 or newer is required.

## How to run

```bash
docker run \
    -d \
    --restart=unless-stopped \
    -p 8080:8080 \
    -e FORGEJO_URL="http://forgejo:3000" \
    -e FORGEJO_TOKEN="your Forgejo token" \
    -e GITHUB_USERNAME="your github user" \
    ghcr.io/jdevera/hubtojo:latest
```

The web status page is available on `/`, the dashboard-friendly JSON stats
endpoint is available on `/stats`, and Prometheus metrics are available on
`/metrics`.

## Status endpoint

`GET /stats` returns the current service state and the latest synchronization
results as JSON:

```bash
curl http://localhost:8080/stats
```

```json
{
  "version": "v0.2.0",
  "service_started_at": "2026-08-27T17:14:07Z",
  "sync_interval_seconds": 3600,
  "next_run_at": "2026-08-27T18:14:07Z",
  "last_run": {
    "run_count": 4,
    "status": "completed_with_errors",
    "started_at": "2026-08-27T17:14:07Z",
    "finished_at": "2026-08-27T17:14:19Z",
    "duration_seconds": 12.4,
    "total_read": 5,
    "owned_discovered": 3,
    "starred_discovered": 4,
    "duplicates": 2,
    "created": 2,
    "skipped": 2,
    "would_create": 0,
    "failed": 1,
    "deferred": 0,
    "starred_backlog": 1,
    "starred_enabled": true,
    "starred_organization": {
      "name": "github-stars",
      "result": "existing"
    },
    "created_repositories": [
      "example/new-repository",
      "example/another-repository"
    ],
    "would_create_repositories": null,
    "deferred_repositories": null,
    "failed_repositories": [
      {
        "name": "example/failed-repository",
        "error": "migration failed"
      }
    ]
  }
}
```

Top-level fields:

| Field | Meaning |
|-------|---------|
| `version` | HubToJo version embedded when the image was built |
| `service_started_at` | Service start time in RFC 3339 format |
| `sync_interval_seconds` | Configured delay between synchronization runs |
| `next_run_at` | Scheduled start time of the next run; absent while running and in one-shot mode |
| `current_run` | Present while a run is active; includes its number, `running` status, and start time |
| `last_run` | Most recently completed run; remains available while the next run is active |

Run statuses:

| Status | Meaning |
|--------|---------|
| `running` | A synchronization is in progress |
| `success` | The run completed without repository failures |
| `completed_with_errors` | The run completed, but one or more repositories failed |
| `error` | The run itself failed, such as when the GitHub repository list could not be fetched; details are in `error` |

The repository arrays identify newly created mirrors, dry-run candidates,
repositories deferred by the starred creation limit, and individual failures.
Empty arrays are currently encoded as `null`, so clients should treat `null` as
an empty list. `owned_discovered` and `starred_discovered` are source counts;
`total_read` is the number remaining after deduplication. `starred_backlog`
counts starred repositories that were not confirmed as mirrored, including
dry-run candidates, failures, and deferred repositories. Result counters are
finalized when a run completes; `current_run` reports lifecycle state rather
than live progress.

The web page, `/stats`, and `/metrics` do not require authentication. They never
include configured tokens, but the web page and `/stats` may contain sensitive
repository names and error messages. Protect port 8080 with network policy or
an authenticated reverse proxy when it should not be publicly accessible.

## Prometheus metrics

`GET /metrics` exposes synchronization and standard Go process metrics in the
Prometheus exposition format:

```bash
curl http://localhost:8080/metrics
```

Configure Prometheus to scrape the HubToJo container directly:

```yaml
scrape_configs:
  - job_name: hubtojo
    static_configs:
      - targets:
          - hubtojo:8080
```

HubToJo exports these application metrics:

| Metric | Meaning |
|--------|---------|
| `hubtojo_build_info` | Build information labeled by HubToJo version |
| `hubtojo_run_in_progress` | `1` while synchronization is running, otherwise `0` |
| `hubtojo_current_run_start_timestamp_seconds` | Start time of the active run, or `0` while idle |
| `hubtojo_runs_total` | Completed runs labeled by `status` |
| `hubtojo_repository_results_total` | Repository outcomes labeled by `result` |
| `hubtojo_last_run_status` | Last completed status represented as one-hot gauges |
| `hubtojo_last_run_repository_results` | Repository outcomes from the last completed run |
| `hubtojo_last_run_repositories_discovered` | Repositories found in the last run, labeled by `source` (`owned` or `starred`) |
| `hubtojo_starred_repository_backlog` | Starred repositories not confirmed as mirrored in the last run |
| `hubtojo_organization_operations_total` | Starred archive organization checks labeled by `result` |
| `hubtojo_last_run_timestamp_seconds` | Completion time of the last run, or `0` before the first run |
| `hubtojo_next_run_timestamp_seconds` | Scheduled next run, or `0` while running and in one-shot mode |
| `hubtojo_sync_interval_seconds` | Configured synchronization interval |
| `hubtojo_run_duration_seconds` | Histogram of completed run durations |

Run status labels are `success`, `completed_with_errors`, `error`, and
`unknown`. Repository result labels are `created`, `skipped`, `would_create`,
and `failed`. Organization result labels are `created`, `existing`, and
`failed`; a dry-run `would_create` organization is reported through `/stats`
without incrementing that counter. Repository names and error strings are
deliberately excluded from metric labels to keep cardinality bounded; use
`/stats` or logs for those details.

Useful PromQL queries include:

```promql
# Repository outcomes over the last 24 hours
sum by (result) (increase(hubtojo_repository_results_total[24h]))

# Last run failed or completed with repository errors
hubtojo_last_run_status{status=~"completed_with_errors|error"} == 1

# Scheduler is overdue while the process is idle
hubtojo_sync_interval_seconds > 0
and hubtojo_run_in_progress == 0
and (time() - hubtojo_last_run_timestamp_seconds > 2 * hubtojo_sync_interval_seconds)

# Prometheus cannot scrape HubToJo
up{job="hubtojo"} == 0

# Starred repositories are waiting to be mirrored
hubtojo_starred_repository_backlog > 0
```

Metrics live in process memory and reset when HubToJo restarts. This is normal
for Prometheus counters: use `rate()` or `increase()` instead of treating raw
`_total` values as durable lifetime totals. Prometheus retains previously
scraped samples, and `process_start_time_seconds` identifies the current
process lifetime. A run interrupted by process termination is not recorded as
completed; logs remain the appropriate source for repository-level audit
details.

## Releases

Release images are published to the GitHub Container Registry for Linux AMD64
and ARM64. Publishing a GitHub Release with a semantic version tag builds and
publishes matching image tags:

```bash
gh release create v0.2.0 --generate-notes
```

This publishes `ghcr.io/jdevera/hubtojo:0.2.0`, `:0.2`, and `:latest`. The Git
tag is also embedded in the binary as its version. Starting with `v1.0.0`,
releases also publish a major-only tag such as `:1`. Prereleases publish only
their exact version and do not update floating tags or `:latest`.

## What can be mirrored

- Public Repos: All public repos of the given user, **excluding forks**
- Private Repos: All private repos of the given user
- Forks: All forks of the given user (they are always public)
- Starred Repos: All repositories starred by the authenticated GitHub user

Each group can be enabled or disabled with environment variables. Starred
repositories are an explicit archive selection, so the public, private, and
fork filters do not apply to them.

## Starred repository archive

Starred mirroring is disabled by default. Enable it with a GitHub token:

```bash
-e GITHUB_TOKEN="your GitHub token" \
-e HUBTOJO_MIRROR_STARRED_REPOS=true
```

HubToJo validates that the token belongs to `GITHUB_USERNAME` and reads the
authenticated user's stars. A fine-grained token needs read access to starring;
private starred repositories are included only when the token can access them.

Third-party stars are mirrored into one Forgejo organization, `github-stars` by
default. HubToJo creates the organization when it is missing. If it already
exists, the Forgejo token's user must be an organization owner because Forgejo
requires ownership for repository migrations. In dry-run mode, a missing
organization is reported but not created.

Destinations use `github-stars/GITHUB_OWNER__REPOSITORY`, preserving the GitHub
owner and avoiding clashes between repositories with the same name. Forgejo
limits repository names to 100 characters. Names over that limit keep the
owner, truncate the repository portion, and append the immutable GitHub
repository ID, for example `owner__truncated--gh123456789`.

Repositories owned by `GITHUB_USERNAME` keep the existing personal Forgejo
destination even when starred, and duplicate GitHub repository IDs are handled
once per run. Unstarring or removing a source repository never deletes or
disables an existing Forgejo mirror. HubToJo also never deletes or renames the
archive organization.

HubToJo does not persist a GitHub-ID-to-Forgejo-name index. If a GitHub
repository is renamed or transferred, a later run can create a mirror at the
new readable destination; the old mirror remains because archival cleanup is
intentionally non-destructive.

By default, at most 25 missing starred repositories are attempted per run.
Existing mirrors do not consume the limit; failed migration attempts do. Owned
repositories are never limited. Set `HUBTOJO_MAX_STARRED_CREATES_PER_RUN=0` for
unlimited starred creation attempts.

## Parameters

| Parameter                       | Description                                                                      | Mandatory | Default |
|---------------------------------|----------------------------------------------------------------------------------|-----------|---------|
| `FORGEJO_URL`                   | The URL of the Forgejo instance that will mirror the repositories                | Yes       |         |
| `FORGEJO_TOKEN`                 | The token to use when authenticating with the Forgejo API                        | Yes       |         |
| `GITHUB_USERNAME`               | The GitHub username to mirror repositories from                                  | Yes       |         |
| `GITHUB_TOKEN`                  | GitHub token; required for private or starred repositories                       | No        |         |
| `HUBTOJO_MIRROR_PUBLIC_REPOS`   | Set to false or 0 to not mirror public repositories. This does not affect forks. | No        | `true`  |
| `HUBTOJO_MIRROR_PRIVATE_REPOS`  | Set to true or 1 to mirror private repositories                                  | No        | `false` |
| `HUBTOJO_MIRROR_FORKS`          | Set to true or 1 to mirror forks                                                 | No        | `false` |
| `HUBTOJO_MIRROR_STARRED_REPOS`  | Set to true or 1 to mirror repositories starred by the authenticated user        | No        | `false` |
| `HUBTOJO_STARRED_ORG`           | Forgejo organization used for third-party starred repositories                   | No        | `github-stars` |
| `HUBTOJO_MAX_STARRED_CREATES_PER_RUN` | Maximum new starred migration attempts per run; 0 is unlimited             | No        | `25`    |
| `HUBTOJO_DRY_RUN`               | Set to true or 1 to skip the write operations and instead just log them          | No        | `false` |
| `HUBTOJO_NUM_WORKERS`           | The number of concurrent workers to use when mirroring repositories              | No        | `5`     |
| `HUBTOJO_SYNC_INTERVAL`         | The interval in seconds to wait between syncs. Set to 0 to run only once         | No        | `3600`  |
| `HUBTOJO_RUN_TIMEOUT`           | Maximum duration in seconds for startup checks and each synchronization run       | No        | `3600`  |
| `HUBTOJO_WEB_ADDR`              | The address for the status page and stats endpoint                               | No        | `:8080` |


## Custom certificates

If your Forgejo instance is served with a self-signed certificate, or you have a custom CA, `hubtojo` will refuse to connect.

You can provide a custom CA certificate by mounting it in the container. Make sure any additional certificates you want to be trusted are available in the `/usr/local/share/ca-certificates` directory.

With Docker:
```bash
-v /dir/with/certificates:/usr/local/share/ca-certificates:ro
```

With Docker Compose:

```yaml
volumes:
  - /dir/with/certificates:/usr/local/share/ca-certificates:ro
```

The container will automatically trust any certificates in that directory.

## Development

Install [prek](https://prek.j178.dev/), then install the repository hooks:

```bash
brew install prek
prek install
```

Run every check against the full repository with:

```bash
prek run --all-files
```
