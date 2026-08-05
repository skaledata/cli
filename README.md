# SkaleData CLI (`skale`)

> **Source has moved.** The `skale` CLI now lives in the SkaleData monorepo at
> [`apps/cli`](https://github.com/skaledata/skaledata/tree/main/apps/cli) (SKD-200).
> This repo is retained as the **public release/distribution host** — GitHub
> releases + binaries are published here, and the Homebrew formula points here.
> Releases are cut from the monorepo (`apps/cli/v*` tags); this repo's own build
> workflows were removed. Issues/PRs against the CLI go in the monorepo.

Manage clusters, apps, and deployments from your terminal.

## Install

### Homebrew (macOS/Linux) — recommended

```bash
brew install skaledata/tap/skale
```

### Binary

Download the latest release from [Releases](https://github.com/skaledata/cli/releases) (assets are named `skale_<version>_<os>_<arch>.tar.gz`).

> `go install` is not supported — the source lives in a private monorepo. Use Homebrew or a binary download.

## Usage

Run `skale --help`, or see the docs at [skaledata.com](https://skaledata.com).
