# HubToTea 🐙2️⃣🍵: Sync Github repos to Gitea

This program will create Gitea mirrors of the github repositories you specify.

Best run with docker.

## How to run

```bash
docker run \
    -d \
    --restart=unless-stopped \
    -e GITEA_URL="http://gitea:3000" \
    -e GITEA_TOKEN="your gitea token" \
    -e GITHUB_USER="your github user" \
    jdevera/hubtotea:latest
```


## What can be mirrored

- Public Repos: All public repos of the given user, **excluding forks**
- Private Repos: All private repos of the given user
- Forks: All forks of the given user (they are always public)

Each of these groups can be enabled or disabled with the environment variables.

## Parameters

| Parameter                       | Description                                                                      | Mandatory | Default |
|---------------------------------|----------------------------------------------------------------------------------|-----------|---------|
| `GITEA_URL`                     | The URL of the Gitea instance that will be mirroring the repositories            | Yes       |         |
| `GITEA_TOKEN`                   | The token to use when authenticating with the Gitea API                          | Yes       |         |
| `GITHUB_USER`                   | The github username to mirror repositories from                                  | Yes       |         |
| `GITHUB_TOKEN`                  | A Github token is required only when working with private repositories           | No        |         |
| `HUBTOTEA_MIRROR_PUBLIC_REPOS`  | Set to false or 0 to not mirror public repositories. This does not affect forks. | No        | `true`  |
| `HUBTOTEA_MIRROR_PRIVATE_REPOS` | Set to true or 1 to mirror private repositories                                  | No        | `false` |
| `HUBTOTEA_MIRROR_FORKS`         | Set to true or 1 to mirror forks                                                 | No        | `false` |
| `HUBTOTEA_DRY_RUN`              | Set to true or 1 to skip the write operations and instead just log them          | No        | `false` |
| `HUBTOTEA_NUM_WORKERS`          | The number of concurrent workers to use when mirroring repositories              | No        | `5`     |
| `HUBTOTEA_SYNC_INTERVAL`        | The interval in seconds to wait between syncs. Set to 0 to run only once         | No        | `3600`  |


