package airflow

// Dockerfile template for local Airflow 3 projects. The SkaleData base
// image's ONBUILD layer handles requirements.txt + packages.txt at the
// build-context root, so the customer's Dockerfile collapses to a FROM
// line plus comments.
const DockerfileTemplate = `FROM ghcr.io/skaledata/airflow:3.2.2

# The SkaleData Airflow base image pre-installs:
#   - apache-airflow + apache-airflow-providers-airbyte
#   - skale-airflow-plugins (Airbyte bearer-auth shim, etc.)
#   - a default webserver_config.py
#
# It also auto-picks-up two files from this build context (no COPY/RUN needed):
#   requirements.txt - pip-installed under upstream Airflow constraints
#   packages.txt     - apt-installed
#
# DAGs are synced into your deployed cluster from blob storage by SkaleData's
# sidecar. For local dev, the SkaleData CLI mounts ./dags into the container.
#
# Project-specific Airflow plugins go in ./plugins (also mounted locally,
# COPY them in here if you need them in your deployed image).
#
# See https://docs.skaledata.com/airflow-base for everything that's baked in.
`

// ComposeTemplate is the docker-compose.yml for local Airflow 3 development.
// Services: postgres, airflow-init (DB migrate), api-server, scheduler,
// dag-processor, triggerer. Uses LocalExecutor with FAB auth manager
// configured for no-login local dev.
const ComposeTemplate = `x-airflow-common: &airflow-common
  build:
    context: ..
    dockerfile: Dockerfile
  env_file:
    - path: ../.env
      required: false
  environment: &airflow-env
    AIRFLOW__CORE__EXECUTOR: LocalExecutor
    AIRFLOW__DATABASE__SQL_ALCHEMY_CONN: postgresql+psycopg2://airflow:airflow@postgres:5432/airflow
    AIRFLOW__CORE__FERNET_KEY: ''
    AIRFLOW__CORE__LOAD_EXAMPLES: 'false'
    AIRFLOW__CORE__DAGS_ARE_PAUSED_AT_CREATION: 'false'
    AIRFLOW__CORE__AUTH_MANAGER: airflow.providers.fab.auth_manager.fab_auth_manager.FabAuthManager
    AIRFLOW__CORE__EXECUTION_API_SERVER_URL: 'http://api-server:8080/execution/'
    AIRFLOW__API_AUTH__JWT_SECRET: 'skale-local-dev-jwt-secret'
    AIRFLOW__SCHEDULER__ENABLE_HEALTH_CHECK: 'true'
  volumes:
    - ../dags:/opt/airflow/dags
    - ../plugins:/opt/airflow/plugins
    - ../tests:/opt/airflow/tests
    - airflow-logs:/opt/airflow/logs
    - ../.skale/gcp-credentials.json:/tmp/gcp-credentials.json:ro
    - ../.skale/gcp-access-token:/tmp/gcp-access-token:ro
    - ../.skale/azure-token.json:/tmp/azure-token.json:ro
  depends_on:
    postgres:
      condition: service_healthy

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: airflow
      POSTGRES_PASSWORD: airflow
      POSTGRES_DB: airflow
    ports:
      - "127.0.0.1:5432:5432"
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U airflow"]
      interval: 5s
      retries: 5
    restart: unless-stopped

  airflow-init:
    <<: *airflow-common
    entrypoint: /bin/bash
    command:
      - -c
      - |
        airflow db migrate
        airflow users create --role Admin --username admin --password admin \
          --email admin@localhost --firstname Admin --lastname User 2>/dev/null || true
    environment:
      <<: *airflow-env
      _AIRFLOW_DB_MIGRATE: 'true'

  api-server:
    <<: *airflow-common
    command: api-server
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:8080/api/v2/version || exit 1"]
      interval: 10s
      timeout: 10s
      retries: 12
      start_period: 30s
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      airflow-init:
        condition: service_completed_successfully

  scheduler:
    <<: *airflow-common
    command: scheduler
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:8974/health || exit 1"]
      interval: 10s
      timeout: 10s
      retries: 12
      start_period: 30s
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      airflow-init:
        condition: service_completed_successfully

  dag-processor:
    <<: *airflow-common
    command: dag-processor
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      airflow-init:
        condition: service_completed_successfully

  triggerer:
    <<: *airflow-common
    command: triggerer
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      airflow-init:
        condition: service_completed_successfully

volumes:
  postgres-data:
  airflow-logs:
`

// ExampleDAG demonstrates dynamic task mapping with the TaskFlow API.
const ExampleDAG = `"""
Example DAG — demonstrates dynamic task mapping.

Triggers 120 parallel tasks that each sleep for a random duration.
Great for testing autoscaling and concurrency. Trigger manually from the UI.

Modify this file or add new DAGs in the dags/ directory.
"""
from datetime import datetime

from airflow import DAG
from airflow.sdk import task

with DAG(
    dag_id="example_dag",
    schedule=None,
    start_date=datetime(2025, 1, 1),
    catchup=False,
    tags=["example"],
):

    @task
    def sleepy_task(i: int):
        import logging
        import random
        import time

        logger = logging.getLogger(__name__)
        seconds = random.randint(0, 25)
        logger.info(f"Task {i}: sleeping for {seconds}s")
        time.sleep(seconds)
        logger.info(f"Task {i}: done")

    sleepy_task.expand(i=list(range(120)))
`

const RequirementsTxt = `# Python dependencies for your Airflow image.
#
# Installed automatically by the SkaleData base image's ONBUILD layer
# (pip install -r requirements.txt under the upstream Airflow constraints
# file, so customer deps can't break the base image's pinned dep tree).
#
# Examples:
#   apache-airflow-providers-google
#   pandas
#   dbt-snowflake
`

const PackagesTxt = `# OS packages for your Airflow image (one per line, apt-get install names).
#
# Installed automatically by the SkaleData base image's ONBUILD layer.
#
# Examples:
#   curl
#   jq
#   build-essential
`

const ReadmeTemplate = `# SkaleData Airflow project

Local development uses Docker Compose (orchestrated by the SkaleData CLI).
Production runs on a SkaleData-managed Airflow cluster.

## Project layout

` + "```" + `
.
├── Dockerfile            # FROM ghcr.io/skaledata/airflow:<version> + comments
├── README.md             # this file
├── requirements.txt      # pip deps — auto-installed via ONBUILD
├── packages.txt          # apt deps — auto-installed via ONBUILD
├── dags/                 # your DAGs (example_dag.py to get you started)
├── plugins/              # project-specific Airflow plugins
└── tests/                # DAG tests (pytest)
` + "```" + `

The SkaleData base image (` + "`ghcr.io/skaledata/airflow`" + `) pre-installs
common platform glue (Airbyte bearer-auth shim under
` + "`skale.providers.airbyte.*`" + `, default webserver_config.py, etc.) — see
[docs.skaledata.com/airflow-base](https://docs.skaledata.com/airflow-base)
for the full list and how to override.

## Local dev

` + "```bash" + `
skale airflow start          # build image + start all services
skale airflow stop           # graceful stop (volumes preserved)
skale airflow restart        # restart without rebuild
skale airflow kill           # nuke containers + volumes (fresh start)
skale airflow logs -f        # tail logs from all services
skale airflow bash           # interactive shell in the scheduler
` + "```" + `

Once running:

- **Airflow UI:** http://localhost:8080 (no login screen in local dev)
- **Postgres:** localhost:5432 (` + "`airflow / airflow`" + `)

## Adding dependencies

- **Python:** add to ` + "`requirements.txt`" + `, then ` + "`skale airflow start`" + ` to rebuild.
- **OS:** add to ` + "`packages.txt`" + `, then rebuild. Same flow.

## Deploying to a SkaleData cluster

` + "```bash" + `
skale airflow deploy
` + "```" + `

This builds the image (with the same files you have locally), pushes to your
cluster's Artifact Registry, and triggers a rolling restart of the scheduler,
api-server, dag-processor, and triggerer.

## Tests

` + "```bash" + `
skale airflow run pytest tests/
` + "```" + `
`

const Gitignore = `__pycache__/
*.pyc
.env
.skale/
logs/
airflow.db
airflow.cfg
`

const Dockerignore = `.git
.env
.skale/
__pycache__
*.pyc
logs/
airflow.db
.DS_Store
`
