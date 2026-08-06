<h1 align="center">
  <img src=".github/silo-word.svg" alt="SILO" height="80">
</h1>


<p align="center">
  <strong>Conservatively maintained S3-compatible object storage</strong><br>
  Security maintenance, versioned release artifacts, and operational continuity for existing deployments.
</p>

<p align="center">
  <a href="https://silo.pgsty.com/">Website</a> ·
  <a href="https://silo.pgsty.com/docs/">Documentation</a> ·
  <a href="https://silo.pgsty.com/download/">Download</a> ·
  <a href="https://silo.pgsty.com/blog/">Blog</a> ·
  <a href="https://github.com/pgsty/silo/releases">Releases</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="README_ZH.md">中文</a>
</p>

<p align="center">
  <a href="https://github.com/pgsty/silo/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/pgsty/silo?include_prereleases&label=release&logo=github"></a>
  <a href="https://hub.docker.com/r/pgsty/silo"><img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/pgsty/silo?logo=docker"></a>
  <a href="go.mod"><img alt="Go Version" src="https://img.shields.io/github/go-mod/go-version/pgsty/silo?logo=go"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPLv3-blue"></a>
</p>

> [!IMPORTANT]
> Silo is an independent, community-maintained fork of the open-source MinIO server, published by [Pigsty](https://pigsty.io) from [`pgsty/silo`](https://github.com/pgsty/silo). It is not affiliated with, endorsed by, or sponsored by MinIO, Inc. “MinIO” is used only to identify the upstream project and compatibility lineage.

> [!NOTE]
> This repository was renamed from `pgsty/minio` to `pgsty/silo`, and its default branch from `master` to `main`, on 2026-08-06. If you need the artifacts maintained under the original MinIO identity, they are on the archived [`minio`](https://github.com/pgsty/silo/tree/minio) branch and in the releases up to [`RELEASE.2026-08-04T00-00-00Z`](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-08-04T00-00-00Z); those assets and the `docker.io/pgsty/minio` image stay published and unmodified. The rename changed product and artifact names only — `MINIO_*` variables, `minio_*` metrics, `x-minio-*` headers, `/minio/*` routes, and the on-disk format are unchanged.

## Overview

Silo maintains one downstream release line derived from the open-source MinIO server. It provides maintained builds and release artifacts for existing MinIO-compatible deployments after upstream community distribution ended. Pigsty uses Silo for object storage, including as an optional PostgreSQL backup repository.

The official project portal is [silo.pgsty.com](https://silo.pgsty.com/). It brings documentation, downloads, release and security notes, and project background together. English is served at the site root; Chinese is available under [/zh/](https://silo.pgsty.com/zh/).

## Find the Right Resource

| Looking for | Canonical location |
| :-- | :-- |
| Project overview and navigation | [Silo Website](https://silo.pgsty.com/) ([中文](https://silo.pgsty.com/zh/)) |
| Installation methods and downloads | [Download & Install](https://silo.pgsty.com/download/) ([中文](https://silo.pgsty.com/zh/download/)) |
| Operations, administration, development, and reference | [Documentation](https://silo.pgsty.com/docs/) ([中文](https://silo.pgsty.com/zh/docs/)) |
| Project news, release notes, and security notes | [Blog](https://silo.pgsty.com/blog/), including [releases](https://silo.pgsty.com/blog/release/) and [security](https://silo.pgsty.com/blog/security/) |
| Versioned binaries, checksums, and source archives | [GitHub Releases](https://github.com/pgsty/silo/releases) |
| Bug reports and feature discussions | [GitHub Issues](https://github.com/pgsty/silo/issues) |
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

- the `github.com/minio/minio` module path and `github.com/minio/*` import paths;
- MinIO-compatible S3 APIs, wire behavior, `MINIO_*` environment variables, metrics, protocol headers, reserved routes, and storage metadata;
- `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tags and legacy `minio …` container argv translation.

Silo-owned delivery surfaces use the `silo` executable, package, service, Helm chart, and `pgsty/silo` container image. Native artifacts intentionally do not install a `minio` binary alias.

Compatibility is the default constraint. Silo preserves existing wire, client, configuration, and operational behavior whenever doing so remains safe. Compatibility is broken only when necessary to close a major security issue, and the release notes must identify the affected behavior and migration path. Treat each release as a downstream upgrade: pin versions, review [release notes](https://silo.pgsty.com/blog/release/) and [security advisories](docs/security/advisories.md), keep a rollback path, and test before production use.

## Downloads and Release Artifacts

Use [Download & Install](https://silo.pgsty.com/download/) to choose an installation method. GitHub Releases remains the source for versioned server binaries, checksums, and source archives.

| Artifact | Location |
| :-- | :-- |
| Source | [`github.com/pgsty/silo`](https://github.com/pgsty/silo) |
| Container image | [`pgsty/silo`](https://hub.docker.com/r/pgsty/silo), multi-arch for `linux/amd64` and `linux/arm64` |
| Server binaries and checksums | [GitHub Releases](https://github.com/pgsty/silo/releases) for Linux, macOS, and Windows on `amd64` and `arm64` |
| Linux packages | RPM, DEB, and APK artifacts, also distributed through the [Pigsty repository](https://pigsty.io/docs/repo/) |
| Client | [`pgsty/mc`](https://github.com/pgsty/mc), bundled in the container as `mcli` with an `mc` compatibility alias |
| Console | [`pgsty/silo-console`](https://github.com/pgsty/silo-console), embedded through the compatibility import path `github.com/minio/console` |
| Shared library | [`pgsty/silo-pkg`](https://github.com/pgsty/silo-pkg) v3.11.0, consumed through a `replace` directive while preserving the `github.com/minio/pkg/v3` import path |

Each new release publishes per-archive and per-package SPDX JSON SBOMs. The archive and package checksum manifests have detached keyless Sigstore bundles, while GitHub artifact attestations record signed provenance for every downloadable artifact and the multi-architecture container image.

After downloading an archive and its release files, verify integrity, the
published SBOM, the signed manifest, and build provenance independently:

```bash
# Integrity: choose the line for the artifact you downloaded.
grep -F '  silo_<version>_linux_amd64.tar.gz' \
  silo_<version>_checksums.txt | sha256sum --check

# The archive SBOM is a separate checksummed release artifact.
grep -F '  silo_<version>_linux_amd64.tar.gz.sbom.json' \
  silo_<version>_checksums.txt | sha256sum --check

# Signature over the archive/SBOM checksum manifest.
cosign verify-blob \
  --bundle silo_<version>_checksums.txt.sigstore.json \
  --certificate-identity-regexp \
    '^https://github.com/pgsty/(minio|silo)/\.github/workflows/release\.yml@refs/(tags/RELEASE\..+|heads/(master|main))$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  silo_<version>_checksums.txt

# Signed build provenance (online verification against this repository).
gh attestation verify silo_<version>_linux_amd64.tar.gz \
  --repo pgsty/silo
```

For packages, use `silo_<version>_packages_checksums.txt` and its adjacent
Sigstore bundle with the same identity and issuer constraints. Inspect the
verified SPDX JSON SBOM with your preferred SPDX tooling. Verify the
multi-architecture container provenance by digest:

```bash
gh attestation verify \
  oci://index.docker.io/pgsty/silo@sha256:<manifest-digest> \
  --repo pgsty/silo
```

The platform SBOM attestations are attached to the `amd64` and `arm64` image
digests rather than the multi-architecture manifest. Verify each one explicitly:

```bash
gh attestation verify \
  oci://index.docker.io/pgsty/silo@sha256:<platform-digest> \
  --repo pgsty/silo \
  --predicate-type https://spdx.dev/Document/v2.3
```

Verification by digest avoids trusting a mutable image tag.

### Native package migration

The `silo` RPM, DEB, and APK do not declare `Provides`, `Obsoletes`,
`Replaces`, or package-level `Conflicts` against `minio`. They can therefore be
installed beside an existing MinIO package without silently replacing it. The
two systemd units conflict at runtime, so switch them explicitly rather than
starting both.

Before switching, record the old unit's enabled/active state and `User`/`Group`,
and back up `/etc/default/minio`. Silo reads that legacy defaults file first and
then `/etc/default/silo`; administrator-set values in the latter take
precedence. If the existing data must continue to run under its original
UID/GID, create `/etc/systemd/system/silo.service.d/10-legacy-user.conf`:

```ini
[Service]
User=<legacy-user>
Group=<legacy-group>
```

Run `systemctl daemon-reload`, then disable and stop `minio.service` before
enabling and starting `silo.service`. Verify health, S3, Admin API, metrics, and
logs before masking or uninstalling the old service. Do not recursively change
data ownership as part of the package migration; keep the old package and unit
available during the rollback window.

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
  docker.io/pgsty/silo:latest server /data --console-address ":9001"
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
go build -o silo .
./silo --version
```

For other installation paths—including native packages, binaries, Podman, Kubernetes, source, and Pigsty Ansible—use [Download & Install](https://silo.pgsty.com/download/). For production deployment and administration, start with the [Silo documentation](https://silo.pgsty.com/docs/). Pigsty users can also use the [Pigsty MinIO module](https://pigsty.io/docs/minio/).

## Security

Security fixes target the active development branch and are recorded in the [advisory log](docs/security/advisories.md) and the portal's [security notes](https://silo.pgsty.com/blog/security/). Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md) and [`VULNERABILITY_REPORT.md`](VULNERABILITY_REPORT.md). Report issues that also affect upstream MinIO there as well.

## Contributing

Useful contributions include security and dependency updates, reproducible bug fixes, tests, release automation, packaging, and documentation.

Issues and pull requests should include the affected version, reproduction steps, impact, expected behavior, tests, and compatibility notes. Discuss large changes in an issue first.

There is no CLA: contributions are accepted inbound=outbound under the project license (AGPL-3.0-or-later) and contributors keep their copyright. Every commit must be signed off (`git commit -s`) per the [Developer Certificate of Origin](https://developercertificate.org/); see [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Background

This project was created in response to changes in the upstream community distribution and maintenance model. The maintainer’s analysis, alternatives considered, and early maintenance record are documented below:

| Essay | Subject |
| :-- | :-- |
| [MinIO Is Dead](https://silo.pgsty.com/blog/post/minio-is-dead/) | Changes to the upstream project and distribution model |
| [MinIO Is Dead, Long Live MinIO](https://silo.pgsty.com/blog/post/minio-resurrect/) | Establishing the fork and its release pipeline |
| [Two months into maintaining a MinIO fork](https://silo.pgsty.com/blog/post/minio-promise-kept/) | Initial security and maintenance work |

## License and Trademark

The server remains licensed under the [GNU Affero General Public License v3.0](LICENSE). See [`CREDITS`](CREDITS) and [`NOTICE`](NOTICE) for upstream authorship and attribution. MinIO is a trademark of MinIO, Inc. Silo is an independent community project and is not affiliated with or endorsed by MinIO, Inc.
