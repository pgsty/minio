> [!WARNING]
> **This branch is archived and receives no further changes.**
>
> `minio` holds the final state of this project under the MinIO identity. Development continues on **[`main`](https://github.com/pgsty/silo/tree/main)**, where the project is named **Silo** and the binary, packages, systemd unit, container image, and Helm chart are all named `silo`. On 2026-08-06 the repository was renamed `pgsty/minio` → **[`pgsty/silo`](https://github.com/pgsty/silo)** and its default branch `master` → `main`.
>
> The last release cut from this branch is **[`RELEASE.2026-08-04T00-00-00Z`](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-08-04T00-00-00Z)** (2026-08-04). Its 19 assets carry the `minio` artifact names, and the matching container image is `docker.io/pgsty/minio:RELEASE.2026-08-04T00-00-00Z`. Those artifacts stay published and unmodified — no tag is moved, re-signed, or removed. If you need the artifacts maintained under the original MinIO identity, this branch and the releases up to that tag are where they live. Later releases carry the `silo` names.
>
> **Nothing on the wire changed with the rename.** `MINIO_*` environment variables, `minio_*` Prometheus metrics, `x-minio-*` headers, `/minio/*` routes, the `.minio.sys` on-disk layout, IAM and ARN values, and the `github.com/minio/minio` Go module path are all preserved on `main`. A Silo server reads data written by this release, and the packages install side by side, so migrating or rolling back stays an explicit administrator action.

<h1 align="center">
  <img src=".github/silo-word.svg" alt="SILO" height="80">
</h1>


<p align="center">
  <strong>A conservatively maintained MinIO fork</strong><br>
  Security maintenance, versioned release artifacts, and operational continuity for existing deployments.
</p>

<p align="center">
  <a href="https://silo.pgsty.com/">Website</a> ·
  <a href="https://silo.pgsty.com/docs/">Documentation</a> ·
  <a href="https://silo.pgsty.com/download/">Download</a> ·
  <a href="https://silo.pgsty.com/blog/">Blog</a> ·
  <a href="https://github.com/pgsty/minio/releases">Releases</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="README_ZH.md">中文</a>
</p>

<p align="center">
  <a href="https://github.com/pgsty/minio/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/pgsty/minio?include_prereleases&label=release&logo=github"></a>
  <a href="https://hub.docker.com/r/pgsty/minio"><img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/pgsty/minio?logo=docker"></a>
  <a href="go.mod"><img alt="Go Version" src="https://img.shields.io/github/go-mod/go-version/pgsty/minio?logo=go"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPLv3-blue"></a>
</p>

> [!IMPORTANT]
> Silo is an independent, community-maintained fork of the open-source MinIO server, published by [Pigsty](https://pigsty.io) from [`pgsty/minio`](https://github.com/pgsty/minio). It is not affiliated with, endorsed by, or sponsored by MinIO, Inc. “MinIO” is used only to identify the upstream project and compatibility lineage.

## Overview

Silo maintains one downstream release line based on MinIO [`RELEASE.2025-12-03T12-00-00Z`](https://github.com/minio/minio/releases/tag/RELEASE.2025-12-03T12-00-00Z). It provides maintained builds and release artifacts for existing MinIO-compatible deployments after upstream community distribution ended. Pigsty uses this fork for object storage as an optional PG backup repo.

The official project portal is [silo.pgsty.com](https://silo.pgsty.com/). It brings documentation, downloads, release and security notes, and project background together. English is served at the site root; Chinese is available under [/zh/](https://silo.pgsty.com/zh/).

## Find the Right Resource

| Looking for | Canonical location |
| :-- | :-- |
| Project overview and navigation | [Silo Website](https://silo.pgsty.com/) ([中文](https://silo.pgsty.com/zh/)) |
| Installation methods and downloads | [Download & Install](https://silo.pgsty.com/download/) ([中文](https://silo.pgsty.com/zh/download/)) |
| Operations, administration, development, and reference | [Documentation](https://silo.pgsty.com/docs/) ([中文](https://silo.pgsty.com/zh/docs/)) |
| Project news, release notes, and security notes | [Blog](https://silo.pgsty.com/blog/), including [releases](https://silo.pgsty.com/blog/release/) and [security](https://silo.pgsty.com/blog/security/) |
| Versioned binaries, checksums, and source archives | [GitHub Releases](https://github.com/pgsty/minio/releases) |
| Bug reports and feature discussions | [GitHub Issues](https://github.com/pgsty/minio/issues) |
| License, attribution, and trademark information | [License](https://silo.pgsty.com/about/license/), [Attribution](https://silo.pgsty.com/about/attribution/), and [Trademark](https://silo.pgsty.com/about/trademark/) |

## Maintenance Policy

The active release line covers:

- build and dependency maintenance;
- applicable security fixes and advisories;
- focused fixes for reproducible defects;
- versioned binaries, packages, checksums, and multi-architecture images;
- the web console, client, documentation, and Pigsty integration.

Changes are kept narrow and tested where practical. Maintenance is best effort; no response, remediation, or release schedule is guaranteed.

### Out of scope

- a separate product roadmap, new storage engine, or speculative S3 features;
- broad rewrites or changes that materially expand the downstream delta;
- historical releases or multiple support branches;
- commercial support, SLAs, 24×7 coverage, or SUBNET access;
- deployment design, access control, monitoring, backup, or recovery.

## Compatibility

Silo aims to preserve:

- MinIO-compatible S3 APIs, configuration, environment variables, and CLI conventions;
- `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tags, container entrypoints, and common deployment workflows.

Compatibility is the default constraint. Silo preserves existing wire, client, configuration, and operational behavior whenever doing so remains safe. Compatibility is broken only when necessary to close a major security issue, and the release notes must identify the affected behavior and migration path. Treat each release as a downstream upgrade: pin versions, review [release notes](https://silo.pgsty.com/blog/release/) and [security advisories](docs/security/advisories.md), keep a rollback path, and test before production use.

## Downloads and Release Artifacts

Use [Download & Install](https://silo.pgsty.com/download/) to choose an installation method. GitHub Releases remains the source for versioned server binaries, checksums, and source archives.

| Artifact | Location |
| :-- | :-- |
| Source | [`github.com/pgsty/minio`](https://github.com/pgsty/minio) |
| Container image | [`pgsty/minio`](https://hub.docker.com/r/pgsty/minio), multi-arch for `linux/amd64` and `linux/arm64` |
| Server binaries and checksums | [GitHub Releases](https://github.com/pgsty/minio/releases) for Linux, macOS, and Windows on `amd64` and `arm64` |
| Linux packages | RPM, DEB, and APK artifacts, also distributed through the [Pigsty repository](https://pigsty.io/docs/repo/) |
| Client | [`pgsty/mc`](https://github.com/pgsty/mc), bundled in the container as `mcli` with an `mc` compatibility alias |
| Console | Maintained [`georgmangold/console`](https://github.com/georgmangold/console) fork, embedded in the server build |
| Shared library | [`pgsty/silo-pkg`](https://github.com/pgsty/silo-pkg) v3.7.0, consumed through a `replace` directive while preserving `github.com/minio/pkg/v3` import paths ([release notes](https://silo.pgsty.com/blog/release/pkg-3.7.0/)) |

## Quick Start

For local evaluation:

```bash
mkdir -p data

export MINIO_ROOT_USER=minioadmin
export MINIO_ROOT_PASSWORD=change-me-long-password

docker run -d --name silo \
  -p 9000:9000 \
  -p 9001:9001 \
  -e MINIO_ROOT_USER \
  -e MINIO_ROOT_PASSWORD \
  -v "$PWD/data:/data" \
  pgsty/minio:latest server /data --console-address ":9001"
```

Open the console at <http://localhost:9001>; the S3 API listens on <http://localhost:9000>.

The image includes the compatible client as `mcli`:

```bash
docker exec silo mcli alias set local http://127.0.0.1:9000 \
  "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
docker exec silo mcli mb local/demo
docker exec silo mcli ls local
```

> [!WARNING]
> For production, pin a release, use unique credentials and TLS, monitor the service, keep independent backups, and test recovery.

Build the server from source:

```bash
go build -o minio .
./minio --version
```

For other installation paths—including native packages, binaries, Podman, Kubernetes, source, and Pigsty Ansible—use [Download & Install](https://silo.pgsty.com/download/). For production deployment and administration, start with the [Silo documentation](https://silo.pgsty.com/docs/). Pigsty users can also use the [Pigsty MinIO module](https://pigsty.io/docs/minio/).

## Security

Security fixes target the active `master` branch and are recorded in the [advisory log](docs/security/advisories.md) and the portal's [security notes](https://silo.pgsty.com/blog/security/). Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md) and [`VULNERABILITY_REPORT.md`](VULNERABILITY_REPORT.md). Report issues that also affect upstream MinIO there as well.

## Contributing

Useful contributions include security and dependency updates, reproducible bug fixes, tests, release automation, packaging, and documentation.

Issues and pull requests should include the affected version, reproduction steps, impact, expected behavior, tests, and compatibility notes. Discuss large changes in an issue first.

## Background

This project was created in response to changes in the upstream community distribution and maintenance model. The maintainer’s analysis, alternatives considered, and early maintenance record are documented below:

| Essay | Subject |
| :-- | :-- |
| [MinIO Is Dead](https://silo.pgsty.com/blog/post/minio-is-dead/) | Changes to the upstream project and distribution model |
| [MinIO Is Dead, Long Live MinIO](https://silo.pgsty.com/blog/post/minio-resurrect/) | Establishing the fork and its release pipeline |
| [Two months into maintaining a MinIO fork](https://silo.pgsty.com/blog/post/minio-promise-kept/) | Initial security and maintenance work |

## License and Trademark

The server remains licensed under the [GNU Affero General Public License v3.0](LICENSE). See [`CREDITS`](CREDITS) for upstream authorship and attribution. MinIO is a trademark of MinIO, Inc. Silo and `pgsty/minio` are independent community efforts and are not affiliated with or endorsed by MinIO, Inc.
