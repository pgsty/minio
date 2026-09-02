# Contributors

Silo is maintained by the Pigsty community. This file records the people who have contributed to
this fork since it was established in 2026 — merged code, proposed changes, and the bug reports and
compatibility findings that shaped the releases.

It is maintained by hand and updated at each release. GitHub's own contributor graph is unavailable
here because `pgsty/silo` is a fork, so this file — not that page — is the project's attribution
record. Contributors are listed by GitHub handle. Authorship of every merged commit is preserved in
the Git history and can be verified with `git log --format='%an <%ae>'`.

Upstream MinIO authorship is recorded separately: this fork derives from
[`minio/minio`](https://github.com/minio/minio), [`NOTICE`](NOTICE) retains the upstream product
notice, and the Git history carries the full upstream commit record.

## Code

Contributors whose changes are merged into `main`.

| Contributor | Change | Pull request | Commit |
| :-- | :-- | :-- | :-- |
| [@ZouhairCharef](https://github.com/ZouhairCharef) | Upgraded `go-jose` to v4.1.4 to patch CVE-2026-34986 | [#18](https://github.com/pgsty/silo/pull/18) | [`68e0ba9`](https://github.com/pgsty/silo/commit/68e0ba997) |
| [@mfredenhagen](https://github.com/mfredenhagen) | Bumped `go.opentelemetry.io` to address CVE-2026-39883 | [#19](https://github.com/pgsty/silo/pull/19) | [`1869bd3`](https://github.com/pgsty/silo/commit/1869bd30b) |
| [@pinginfo](https://github.com/pinginfo) | Implemented `Flush` on `trackingResponseWriter`, repairing bucket notification streaming | [#34](https://github.com/pgsty/silo/pull/34) | [`65795ee`](https://github.com/pgsty/silo/commit/65795ee1f) |
| [@waterkip](https://github.com/waterkip) | Repointed documentation links from the upstream domain to the Silo portal | [#41](https://github.com/pgsty/silo/pull/41) | [`d495d30`](https://github.com/pgsty/silo/commit/d495d30d5) |
| [@Dansyuqri](https://github.com/Dansyuqri) | Added `ChecksumType` to the `CompleteMultipartUpload` response | [#57](https://github.com/pgsty/silo/pull/57) | [`d014a12`](https://github.com/pgsty/silo/commit/d014a12cf) |
| [@ycjlin](https://github.com/ycjlin) | `ListObjects` returns `NoSuchBucket` for a prefix on a missing bucket | [#37](https://github.com/pgsty/silo/pull/37) | [`e9c5340`](https://github.com/pgsty/silo/commit/e9c5340be) |
| [@h5vx](https://github.com/h5vx) | Implemented per-bucket CORS: stored configuration, S3 handlers, and request enforcement | [#71](https://github.com/pgsty/silo/pull/71) | [`e4e3007`](https://github.com/pgsty/silo/commit/e4e3007da) |

## Proposed changes

Pull requests that are open for review, or that were closed after informing work that shipped
differently.

| Contributor | Change | Pull request | Status |
| :-- | :-- | :-- | :-- |
| [@magicxor](https://github.com/magicxor) | `DELETE` precondition checks for the `If-Match` header | [#12](https://github.com/pgsty/silo/pull/12) | Open, queued for review |
| [@davinkevin](https://github.com/davinkevin) | Distroless-based Docker image variant | [#21](https://github.com/pgsty/silo/pull/21) | Superseded by the distroless variant shipped in RELEASE.2026-08-06, which the PR anticipated by four months |
| [@lem21h](https://github.com/lem21h) | Assorted fixes and improvements | [#36](https://github.com/pgsty/silo/pull/36) | Closed |
| [@sulin37392](https://github.com/sulin37392) | Dependency updates against the fork | [#8](https://github.com/pgsty/silo/pull/8) | Closed |

## Reports

Bug reports, compatibility findings, and proposals filed against this fork. Several shipped fixes
trace directly back to these: the bundled-client guarantee (#4, #9), the LDAP-over-TLS repair (#15),
the completed native package payload (#33), GPG-signed RPMs (#43), and the upstream migration guide
(#42).

| Contributor | Reported |
| :-- | :-- |
| [@mosesdd](https://github.com/mosesdd) | [#1](https://github.com/pgsty/silo/issues/1) Helm chart availability |
| [@Xavier-777](https://github.com/Xavier-777) | [#2](https://github.com/pgsty/silo/issues/2) Console bucket lifecycle management · [#17](https://github.com/pgsty/silo/issues/17) Log and XML file preview |
| [@jiadzh](https://github.com/jiadzh) | [#3](https://github.com/pgsty/silo/issues/3) Windows build guidance |
| [@TLINDEN](https://github.com/TLINDEN) | [#4](https://github.com/pgsty/silo/issues/4) `mc` missing from released tarballs |
| [@AntonOfTheWoods](https://github.com/AntonOfTheWoods) | [#5](https://github.com/pgsty/silo/issues/5) Upstream Helm chart and operator options |
| [@zylpsrs](https://github.com/zylpsrs) | [#6](https://github.com/pgsty/silo/issues/6) Console missing Tiering and Site Replication |
| [@nsanitate](https://github.com/nsanitate) | [#7](https://github.com/pgsty/silo/issues/7) CNCF Sandbox governance proposal |
| [@makinikm](https://github.com/makinikm) | [#9](https://github.com/pgsty/silo/issues/9) `mc` missing from the Docker image |
| [@magicxor](https://github.com/magicxor) | [#10](https://github.com/pgsty/silo/issues/10) `DeleteObject` ignores the `If-Match` header |
| [@spaceg00se-r](https://github.com/spaceg00se-r) | [#11](https://github.com/pgsty/silo/issues/11) `cpuv1` support · [#14](https://github.com/pgsty/silo/issues/14) Project workflow token failure |
| [@heroes1412](https://github.com/heroes1412) | [#13](https://github.com/pgsty/silo/issues/13) Profile option unusable |
| [@vampywiz17](https://github.com/vampywiz17) | [#15](https://github.com/pgsty/silo/issues/15) LDAP TLS regression breaking Console login on Kubernetes |
| [@davinkevin](https://github.com/davinkevin) | [#20](https://github.com/pgsty/silo/issues/20) Renovate for automated dependency updates |
| [@chalukyaj](https://github.com/chalukyaj) | [#30](https://github.com/pgsty/silo/issues/30) Silo Operator discoverability |
| [@cbornet](https://github.com/cbornet) | [#31](https://github.com/pgsty/silo/issues/31) Multipart uploads with `FULL_OBJECT` CRC32 · [#32](https://github.com/pgsty/silo/issues/32) `ListObjects` bucket-existence semantics |
| [@jvasile](https://github.com/jvasile) | [#33](https://github.com/pgsty/silo/issues/33) `.deb` missing user, group, and default files |
| [@Kesavaambati](https://github.com/Kesavaambati) | [#35](https://github.com/pgsty/silo/issues/35) Community support for the Docker images |
| [@redfoxfox](https://github.com/redfoxfox) | [#38](https://github.com/pgsty/silo/issues/38) Chinese documentation site unreachable |
| [@kuldeep-link11](https://github.com/kuldeep-link11) | [#39](https://github.com/pgsty/silo/issues/39) `notify_nats` rejects JWT credentials files · [#40](https://github.com/pgsty/silo/issues/40) `notify_nats` target changes require a restart |
| [@meesudzu](https://github.com/meesudzu) | [#42](https://github.com/pgsty/silo/issues/42) Migration guide from upstream MinIO |
| [@pmezhuev](https://github.com/pmezhuev) | [#43](https://github.com/pgsty/silo/issues/43) RPM package missing its GPG signature |
| [@kh0mka](https://github.com/kh0mka) | [#51](https://github.com/pgsty/silo/issues/51) Inter-node I/O timeout in `ReadFileStreamHandler` |

## Adding yourself

Contributions are accepted inbound=outbound under AGPL-3.0-or-later with no CLA; see
[`CONTRIBUTING.md`](CONTRIBUTING.md). Merged pull requests are added here at the next release. If a
contribution is missing or recorded incorrectly, open an issue or say so on the pull request and it
will be fixed.
