<h1 align="center">
  <a href="https://silo.pgsty.com/">
    <img src=".github/silo-logo.svg" alt="Silo" width="160">
  </a>
</h1>


<p align="center">
  <strong>S3-compatible object storage — a MinIO fork maintained by PGSTY</strong>
</p>


<p align="center">
  <a href="https://silo.pgsty.com/">Website</a> ·
  <a href="https://silo.pgsty.com/docs/">Documentation</a> ·
  <a href="https://silo.pgsty.com/download/">Download</a> ·
  <a href="https://silo.pgsty.com/tags/silo/">Release Notes</a> ·
  <a href="https://silo.pgsty.com/compatibility/server/">Compatibility</a> ·
  <a href="https://silo.pgsty.com/about/manifesto/">Manifesto</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="README_ZH.md">中文</a>
</p>

<p align="center">
  <a href="https://silo.pgsty.com/"><img alt="Website" src="https://img.shields.io/badge/Website-silo.pgsty.com-1d588c"></a>
  <a href="https://github.com/pgsty/silo/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/pgsty/silo?include_prereleases&label=release&logo=github"></a>
  <a href="https://hub.docker.com/r/pgsty/silo"><img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/pgsty/minio?logo=docker"></a>
  <a href="go.mod"><img alt="Go Version" src="https://img.shields.io/github/go-mod/go-version/pgsty/silo?logo=go"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPLv3-blue"></a>
</p>

> [!IMPORTANT]
> **PGSTY Silo** (hereinafter “Silo”) is an independent, community-maintained fork of the open-source MinIO server, published by [Pigsty](https://pigsty.io) from [`pgsty/silo`](https://github.com/pgsty/silo). It is not affiliated with, endorsed by, or sponsored by MinIO, Inc. “MinIO” is used only to identify the upstream project and compatibility lineage.

> [!NOTE]
> Renamed from `pgsty/minio` to `pgsty/silo`, default branch `master` → `main`, on 2026-08-06. Artifacts under the original MinIO identity stay published on the archived [`minio`](https://github.com/pgsty/silo/tree/minio) branch and in releases up to [`RELEASE.2026-08-04T00-00-00Z`](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-08-04T00-00-00Z).

## Overview

PGSTY SILO keeps one maintained release line of the open-source MinIO server alive after upstream ended community distribution: builds, packages, multi-arch images, security fixes, and the full web console. Pigsty runs it in production as its PostgreSQL backup repository.

It follows one rule — **the product and its delivery surfaces are renamed; the protocol and your data are not.** Everything else lives on [silo.pgsty.com](https://silo.pgsty.com/).

**Related:** [`pgsty/mc`](https://github.com/pgsty/mc) client (shipped as `mcli`) · [`pgsty/silo-console`](https://github.com/pgsty/silo-console) · [`pgsty/silo-pkg`](https://github.com/pgsty/silo-pkg) · [`pgsty/pigsty`](https://github.com/pgsty/pigsty)

<p align="center">
  <img src="https://silo.pgsty.com/images/silo-console/console-metrics-simple.webp" alt="Silo Console">
</p>

## Quick Start

```bash
docker run -d --name silo -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=change-me-long-password \
  -v "$PWD/data:/data" \
  docker.io/pgsty/silo:latest server /data --console-address ":9001"
```

<p align="center">
  <img src="https://silo.pgsty.com/images/silo-console/console-login.webp" alt="Silo Console">
</p>

Console on <http://localhost:9001>, S3 API on <http://localhost:9000>. The image bundles the client as `mcli`:

```bash
docker exec silo mcli alias set local http://127.0.0.1:9000 minioadmin change-me-long-password
docker exec silo mcli mb local/demo && docker exec silo mcli ls local
```

> [!WARNING]
> For production, pin a release, use unique credentials and TLS, monitor the service, keep independent backups, and test recovery. Start from the [documentation](https://silo.pgsty.com/docs/).

## Install

| Method | Where |
| :-- | :-- |
| Container | [`pgsty/silo`](https://hub.docker.com/r/pgsty/silo), multi-arch for `linux/amd64` and `linux/arm64` |
| Binaries | [GitHub Releases](https://github.com/pgsty/silo/releases) — Linux, macOS, Windows on `amd64` and `arm64` |
| Packages | RPM, DEB, and APK, also via the [Pigsty repository](https://pigsty.io/docs/repo/) |
| Kubernetes | Helm chart, see [Download & Install](https://silo.pgsty.com/download/) |
| Source | `go build -o silo . && ./silo --version` |

Every release ships checksums, SPDX SBOMs, Sigstore-signed manifests, and GitHub build attestations. Installation methods and verification commands are documented at [Download & Install](https://silo.pgsty.com/download/); migrating from upstream MinIO — taking over an existing `minio.service` and its `/etc/default/minio`, and keeping data ownership stable with a `/etc/systemd/system/silo.service.d/10-legacy-user.conf` drop-in — is covered by the [migration guide](https://silo.pgsty.com/compatibility/migration/) and the [binary & service notes](https://silo.pgsty.com/compatibility/binary/).

## Compatibility

The S3 API, `MINIO_*` variables, `minio_*` metrics, `x-minio-*` headers, `/minio/*` routes, the `github.com/minio/*` import paths, and the on-disk format (including `.minio.sys`) are preserved and held in place by a CI compatibility check. Only Silo-owned delivery surfaces change: the `silo` executable, package, service, Helm chart, and container image — no `minio` binary alias is installed.

Every divergence from upstream is listed in the code-verified [compatibility audit](https://silo.pgsty.com/compatibility/server/). Treat each release as a downstream upgrade: pin versions, read the [release notes](https://silo.pgsty.com/tags/silo/), and keep a rollback path.

## Security & Contributing

Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md); every fix ships with a public [advisory](https://silo.pgsty.com/blog/security/). Contributions are accepted inbound=outbound under AGPL-3.0-or-later with no CLA — only DCO sign-off (`git commit -s`) is required; see [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Contributors

<table>
  <tr>
    <td align="center" width="150">
      <a href="https://github.com/ZouhairCharef"><img src="https://github.com/ZouhairCharef.png?size=100" width="72" alt="ZouhairCharef"><br><sub><b>@ZouhairCharef</b></sub></a><br><sub>CVE-2026-34986</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/mfredenhagen"><img src="https://github.com/mfredenhagen.png?size=100" width="72" alt="mfredenhagen"><br><sub><b>@mfredenhagen</b></sub></a><br><sub>CVE-2026-39883</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/pinginfo"><img src="https://github.com/pinginfo.png?size=100" width="72" alt="pinginfo"><br><sub><b>@pinginfo</b></sub></a><br><sub>Notification streaming</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/waterkip"><img src="https://github.com/waterkip.png?size=100" width="72" alt="waterkip"><br><sub><b>@waterkip</b></sub></a><br><sub>Documentation links</sub>
    </td>
  </tr>
</table>
<p>
<a href="https://github.com/magicxor"><img src="https://github.com/magicxor.png?size=64" width="44" alt="magicxor" title="@magicxor"></a>
<a href="https://github.com/ycjlin"><img src="https://github.com/ycjlin.png?size=64" width="44" alt="ycjlin" title="@ycjlin"></a>
<a href="https://github.com/h5vx"><img src="https://github.com/h5vx.png?size=64" width="44" alt="h5vx" title="@h5vx"></a>
<a href="https://github.com/Dansyuqri"><img src="https://github.com/Dansyuqri.png?size=64" width="44" alt="Dansyuqri" title="@Dansyuqri"></a>
<a href="https://github.com/davinkevin"><img src="https://github.com/davinkevin.png?size=64" width="44" alt="davinkevin" title="@davinkevin"></a>
<a href="https://github.com/lem21h"><img src="https://github.com/lem21h.png?size=64" width="44" alt="lem21h" title="@lem21h"></a>
<a href="https://github.com/sulin37392"><img src="https://github.com/sulin37392.png?size=64" width="44" alt="sulin37392" title="@sulin37392"></a>
<a href="https://github.com/mosesdd"><img src="https://github.com/mosesdd.png?size=64" width="44" alt="mosesdd" title="@mosesdd"></a>
<a href="https://github.com/Xavier-777"><img src="https://github.com/Xavier-777.png?size=64" width="44" alt="Xavier-777" title="@Xavier-777"></a>
<a href="https://github.com/jiadzh"><img src="https://github.com/jiadzh.png?size=64" width="44" alt="jiadzh" title="@jiadzh"></a>
<a href="https://github.com/TLINDEN"><img src="https://github.com/TLINDEN.png?size=64" width="44" alt="TLINDEN" title="@TLINDEN"></a>
<a href="https://github.com/AntonOfTheWoods"><img src="https://github.com/AntonOfTheWoods.png?size=64" width="44" alt="AntonOfTheWoods" title="@AntonOfTheWoods"></a>
<a href="https://github.com/zylpsrs"><img src="https://github.com/zylpsrs.png?size=64" width="44" alt="zylpsrs" title="@zylpsrs"></a>
<a href="https://github.com/nsanitate"><img src="https://github.com/nsanitate.png?size=64" width="44" alt="nsanitate" title="@nsanitate"></a>
<a href="https://github.com/makinikm"><img src="https://github.com/makinikm.png?size=64" width="44" alt="makinikm" title="@makinikm"></a>
<a href="https://github.com/spaceg00se-r"><img src="https://github.com/spaceg00se-r.png?size=64" width="44" alt="spaceg00se-r" title="@spaceg00se-r"></a>
<a href="https://github.com/heroes1412"><img src="https://github.com/heroes1412.png?size=64" width="44" alt="heroes1412" title="@heroes1412"></a>
<a href="https://github.com/vampywiz17"><img src="https://github.com/vampywiz17.png?size=64" width="44" alt="vampywiz17" title="@vampywiz17"></a>
<a href="https://github.com/chalukyaj"><img src="https://github.com/chalukyaj.png?size=64" width="44" alt="chalukyaj" title="@chalukyaj"></a>
<a href="https://github.com/cbornet"><img src="https://github.com/cbornet.png?size=64" width="44" alt="cbornet" title="@cbornet"></a>
<a href="https://github.com/jvasile"><img src="https://github.com/jvasile.png?size=64" width="44" alt="jvasile" title="@jvasile"></a>
<a href="https://github.com/Kesavaambati"><img src="https://github.com/Kesavaambati.png?size=64" width="44" alt="Kesavaambati" title="@Kesavaambati"></a>
<a href="https://github.com/redfoxfox"><img src="https://github.com/redfoxfox.png?size=64" width="44" alt="redfoxfox" title="@redfoxfox"></a>
<a href="https://github.com/kuldeep-link11"><img src="https://github.com/kuldeep-link11.png?size=64" width="44" alt="kuldeep-link11" title="@kuldeep-link11"></a>
<a href="https://github.com/meesudzu"><img src="https://github.com/meesudzu.png?size=64" width="44" alt="meesudzu" title="@meesudzu"></a>
<a href="https://github.com/pmezhuev"><img src="https://github.com/pmezhuev.png?size=64" width="44" alt="pmezhuev" title="@pmezhuev"></a>
<a href="https://github.com/kh0mka"><img src="https://github.com/kh0mka.png?size=64" width="44" alt="kh0mka" title="@kh0mka"></a>
</p>

GitHub does not generate a contributor graph for forks, so [`CONTRIBUTORS.md`](CONTRIBUTORS.md) — not the Insights page — is this project's attribution record. It names everyone alongside the change or report they contributed.

## Background

Upstream wound down its community edition: the web console was cut back to a stub, prebuilt community binaries stopped, and the community repository was archived. Silo exists to keep those deployments running. The fork is a means, not an identity — if upstream restores its community edition, we will narrow our scope and offer the fixes back.

The [**Manifesto**](https://silo.pgsty.com/about/manifesto/) is the project's public commitment in eleven articles, under one discipline: every article is either something already done with public evidence, or something explicitly refused. In short:

- **Compatibility contract** — the protocol and your data do not change, and every release documents its tested rollback target and path.
- **The license cannot change** — AGPLv3, no CLA, no copyright aggregation; nobody here, ourselves included, holds enough copyright to relicense on everyone else's behalf.
- **The never list**, append-only — no paywalling existing features, no registration wall on downloads, no telemetry (upstream's phone-home paths are removed outright), no CLA, no license change, no trademark enforcement against normal use.
- **Security and release discipline** — a public advisory for every fix, and a release every one to two months, at most a quarter apart. Judge both against the public record.

Essays: [MinIO Is Dead](https://silo.pgsty.com/blog/post/minio-is-dead/) · [Who Takes Over?](https://silo.pgsty.com/blog/post/minio-alternative/) · [Long Live MinIO](https://silo.pgsty.com/blog/post/minio-resurrect/) · [Promise Kept](https://silo.pgsty.com/blog/post/minio-promise-kept/)

## License & Trademark

Silo is [AGPL-3.0-or-later](LICENSE), derived from [`minio/minio`](https://github.com/minio/minio) with upstream copyright and third-party notices preserved in [`NOTICE`](NOTICE) and [`CREDITS`](CREDITS). MinIO is a trademark of MinIO, Inc.; the name is used here only to identify the upstream project and compatibility lineage.

Details: [license](https://silo.pgsty.com/about/license/) · [attribution](https://silo.pgsty.com/about/attribution/) · [trademark](https://silo.pgsty.com/about/trademark/)
