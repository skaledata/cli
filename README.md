# SkaleData CLI

Manage clusters, apps, and deployments from your terminal.

## Install

### Homebrew (macOS/Linux)

```bash
brew install skaledata/tap/skaledata
```

### Go

```bash
go install github.com/skaledata/cli@latest
```

### Binary

Download the latest release from [GitHub Releases](https://github.com/skaledata/cli/releases).

## Quick Start

```bash
# Authenticate
skaledata login

# Or use an API key (CI/headless)
skaledata auth set-key sk_xxx

# List clusters
skaledata clusters list

# Create a cluster
skaledata clusters create --name my-cluster --cloud <cloud-id> --apps airflow

# Scaffold a new Airflow project
skaledata init airflow my-project

# Deploy
cd my-project
skaledata deploy
```

## Commands

| Command | Description |
|---------|-------------|
| `skaledata login` | Log in via browser |
| `skaledata auth set-key` | Store API key for CI |
| `skaledata clusters list` | List all clusters |
| `skaledata clusters create` | Create a cluster (interactive) |
| `skaledata clusters status <id>` | Detailed cluster info |
| `skaledata clusters destroy <id>` | Destroy a cluster |
| `skaledata apps list` | List all applications |
| `skaledata apps add <type>` | Add an app to a cluster |
| `skaledata apps open <cluster-id>` | Open app in browser |
| `skaledata deploy` | Build, push, and deploy |
| `skaledata deploy status` | Latest deploy status |
| `skaledata init airflow <name>` | Scaffold Airflow project |
| `skaledata init airbyte <name>` | Scaffold Airbyte project |
| `skaledata init docs <name>` | Scaffold docs project |
| `skaledata billing` | Show plan and usage |

## Configuration

Config is stored in `~/.config/skaledata/config.yaml`.

Override the API URL with `SKALEDATA_API_URL` or `api_url` in config.

## License

MIT
