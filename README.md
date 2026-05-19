# Whoop Scraper

Go CLI for scraping WHOOP Developer API v2 data into PostgreSQL.

## Features

- OAuth2 authorization flow with refresh-token rotation
- PostgreSQL-backed token storage for container and cron deployments
- Optional AES-256-GCM token encryption at rest
- WHOOP API v2 scraping for profile, body measurements, cycles, recovery, sleep, and workouts
- PostgreSQL upserts with `raw JSONB` snapshots to preserve newly added API fields
- Small static Docker image

WHOOP's current public API docs list OAuth2 plus v2 profile/body, cycle, recovery, sleep, and workout endpoints: <https://developer.whoop.com/api/>.

## Configuration

```bash
export WHOOP_CLIENT_ID='...'
export WHOOP_CLIENT_SECRET='...'

# Either a full DSN:
export WHOOP_DATABASE_URL='postgres://health:password@localhost:5432/health'

# Or split DB settings:
export WHOOP_DB_HOST='localhost'
export WHOOP_DB_PORT='5432'
export WHOOP_DB_NAME='health'
export WHOOP_DB_USER='health'
export WHOOP_DB_PASSWORD='password'
export WHOOP_DB_SCHEMA='whoop'
```

Optional:

```bash
export WHOOP_SCRAPE_DAYS='7'
export WHOOP_ACCESS_TOKEN='bootstrap-access-token'
export WHOOP_REFRESH_TOKEN='bootstrap-refresh-token'
export WHOOP_TOKEN_STORAGE='db' # db or file
export WHOOP_TOKEN_PATH="$HOME/.config/whoop-scraper/tokens.json"

# 32-byte base64 key for AES-256-GCM token encryption:
openssl rand -base64 32
export WHOOP_ENCRYPTION_KEY='...'
```

## Usage

```bash
go run ./cmd/whoop-scraper init-db
go run ./cmd/whoop-scraper auth
go run ./cmd/whoop-scraper auth --status
go run ./cmd/whoop-scraper test-api
go run ./cmd/whoop-scraper scrape --days 30
go run ./cmd/whoop-scraper scrape --start-date 2026-05-01 --end-date 2026-05-19
```

Build:

```bash
go build -o bin/whoop-scraper ./cmd/whoop-scraper
```

Docker:

```bash
docker build -t whoop-scraper .
docker run --rm --env-file .env whoop-scraper init-db
docker run --rm --env-file .env whoop-scraper scrape --days 7
```

## Helm Chart

Release tags publish both runtime artifacts to GitHub Container Registry:

- image: `ghcr.io/nezdemkovski/whoop-scraper:<version>`
- chart: `ghcr.io/nezdemkovski/charts/whoop-scraper:<version>`

Create a release by pushing a semver tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The chart is designed for Argo CD OCI usage like the other homelab apps:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: whoop-scraper
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: homelab
  source:
    repoURL: ghcr.io/nezdemkovski/charts
    chart: whoop-scraper
    targetRevision: 0.1.0
    helm:
      valuesObject:
        image:
          tag: "0.1.0"
        env:
          WHOOP_DB_HOST: whoop-postgres-rw
          WHOOP_DB_NAME: whoop
          WHOOP_DB_USER: whoop
          WHOOP_DB_SCHEMA: whoop
          WHOOP_SCRAPE_DAYS: "7"
        envFrom:
          - secretRef:
              name: whoop-scraper-env
  destination:
    server: https://kubernetes.default.svc
    namespace: whoop
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

The chart creates a CronJob for `scrape` and a Helm hook Job for `init-db`.
Secrets are intentionally external by default; wire them with `envFrom` from
External Secrets or enable `secret.stringData` for simple local installs.

For a one-time historical import, enable the optional backfill Job:

```yaml
backfillJob:
  enabled: true
  days: 500
```

Argo CD will create a normal Kubernetes Job that runs `scrape --days 500` once.
After it reaches `Completed`, set `backfillJob.enabled` back to `false`; the
daily CronJob can stay on a smaller `WHOOP_SCRAPE_DAYS` window.
