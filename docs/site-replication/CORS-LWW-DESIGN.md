# Per-Bucket CORS Site-Replication Convergence Design

## Status

- Issue: [pgsty/silo#75](https://github.com/pgsty/silo/issues/75)
- Baseline: `e4e3007da6d7d1198a6a050e34f84566d40a9654`
- Working branch: `codex/issue-75-cors-hardening`
- Decision: CORS-specific deterministic last-writer-wins register, described below
- Implementation state: B2 is commit `724f8703d`; the final B2+B3 integration is signed commit `0eebc928f` on the PR #80 branch and has passed combined local acceptance
- Release state: PR #80 remains open; nothing is merged, tagged, packaged, published as an image, or deployed
- Final design/implementation review: the B2 implementation was GO; the combined B2+B3 Opus 5 Max review found one test-build conflict and one legacy-metadata load risk, both corrected before combined testing

This document defines the replication state, ordering, persistence, status,
healing, concurrency, compatibility, and test contract for per-bucket CORS.
It is an implementation design record, not public upgrade or rollback guidance.
Public operator documentation belongs in the separate `silo.pgsty.com`
repository.

## Scope

This design covers the current-version CORS path:

```text
PutBucketCors / DeleteBucketCors
    -> persist local CORS state
    -> BucketMetaHook
    -> madmin SRBucketMeta transport
    -> SRPeerReplicateBucketItem dispatch
    -> PeerBucketCorsConfigHandler
    -> SiteReplicationMetaInfo
    -> siteReplicationStatus
    -> latestCORSConfig
    -> healCORSMetadata
```

It also covers retry, duplicate delivery, reordering, equal timestamps,
initial site sync, missed DELETE recovery, cache reload, process restart, and
concurrent CORS mutations on different nodes of one cluster.

The following are deliberately out of scope:

- redesigning the replication semantics of policy, tags, SSE, quota,
  versioning, or Object Lock;
- changing higher-level lifecycle merge semantics or serializing bucket
  deletion against in-flight metadata updates; those follow-ups remain under
  [pgsty/silo#102](https://github.com/pgsty/silo/issues/102);
- mixed-version support that permits CORS writes before every site runs a
  CORS-aware binary;
- public downgrade, rollback, and global-fallback documentation;
- Console UI for bucket CORS.

### Adjacent issue-75 changes in the same candidate

The final issue-75 candidate also contains CORS work outside the LWW register
itself:

- a strict, namespace-tolerant XML wire parser that rejects trailing roots,
  unknown/nested elements, duplicate singleton fields, invalid integer shape,
  and non-whitespace character data;
- Unicode code-point ID counting, exact uppercase S3 methods, non-empty header
  elements, and int32-compatible MaxAge validation;
- a single-`*` matcher and response-selection changes needed to distinguish a
  literal `*` origin from a patterned or explicit `null` match;
- fail-closed metadata-error handling in the HTTP middleware;
- complete allowed-method, explicit MaxAge=0, expose-header, credentials, and
  `Vary` preflight behavior;
- checksum mismatch classification as `BadDigest`; and
- parser, signed-handler, browser-response, and protocol adversarial tests.

Those changes share the same CORS release gate and are present in the reviewed
diff, but they are not part of the replication conflict key or join algorithm.
This document describes them only where they constrain replication validation
or the final verification boundary.

## Confirmed Failures in the Pre-Fix Candidate

The pre-fix issue-75 candidate had four independently reproduced convergence
defects:

1. Heal compared only payloads. If two sites stored identical payload bytes
   with different source timestamps, heal skipped the older site. The sites
   retained different ordering barriers and could disagree on a later delayed
   event.
2. `isBucketMetadataEqual` used `strings.EqualFold` for base64. For example,
   `QQ==` and `qQ==` decode to different bytes but compared equal.
3. Status derived `CorsCfgMismatch` from live payload count and payload set.
   It did not include source timestamp or tombstone state, so it could report
   divergent sites as converged and suppress healing.
4. Equal-timestamp conflicting events had no stable tie-breaker. Peer apply
   accepted whichever event arrived last, while heal selected whichever map
   entry happened to be visited first.

Two additional correctness requirements followed from the state model:

- the read, compare, and save transition must be atomic across nodes in one
  cluster; and
- a successful local PUT or DELETE must advance beyond an already stored
  future source timestamp instead of moving the local barrier backwards.

## Constraints

The minimum fix must satisfy these constraints:

- preserve `madmin.SRBucketMeta` and `madmin.SRBucketInfo` wire schemas;
- preserve the exact source `UpdatedAt` on peer apply and heal;
- distinguish a never-configured bucket from a persisted deletion;
- converge without relying on event arrival order, map iteration order, or a
  particular site being the healer;
- serialize CORS-versus-CORS transitions cluster-wide without introducing a
  broad bucket-metadata redesign;
- reject malformed replication payloads before persistence;
- remain idempotent under retry and initial-sync replay;
- keep the replication state-machine change limited to CORS except for the
  directly shared base64 equality bug; do not infer that the same dirty
  candidate contains no adjacent CORS protocol or middleware changes.

## State Model

For one bucket lineage, the persisted CORS state is:

```text
State = (Payload, SourceUpdatedAt)
```

`BucketMetadata.Created` is not part of the conflict key. It is the lineage
floor used to reject an event from an older incarnation of the bucket.

### State kinds

| Kind | Payload | `CorsConfigUpdatedAt` | Meaning |
| --- | --- | --- | --- |
| Baseline | nil | zero | CORS has never been configured for this bucket lineage |
| Live | non-empty XML bytes | non-zero | A live per-bucket CORS configuration |
| Tombstone | nil | non-zero | CORS was explicitly deleted at the source timestamp |

The baseline uses a zero timestamp deliberately. Defaulting a missing CORS
timestamp to `CreatedAt` would make classification depend on two values that
can be obtained from different cache/disk snapshots. It would also make a
never-configured state indistinguishable from a deletion at bucket creation.

Per-bucket CORS and `CorsConfigUpdatedAt` were introduced together, so there
is no released legacy live-CORS state that requires synthesizing a timestamp.

### Wire canonicalization

A non-nil wire payload must satisfy all of the following:

1. strict standard base64 decoding succeeds;
2. re-encoding the decoded bytes produces exactly the received string;
3. the decoded payload is non-empty;
4. CORS XML parsing succeeds; and
5. `cors.Config.Validate()` succeeds.

The canonical re-encode check rejects ignored newlines and alternate textual
representations. Equality is therefore equality of decoded bytes, with exact
base64 string equality remaining safe for the shared metadata helper.

An invalid wire value is not a candidate winner and is never propagated.
Peer apply rejects it before any metadata write.

A bucket may nevertheless contain a CORS document written by a pre-release,
more lenient build. Loading such metadata keeps policy, lifecycle, versioning,
and the other bucket fields available, but stashes the CORS parse/validation
error and exposes no active CORS config. CORS GET and middleware lookup return
that error, so browser handling fails closed. A valid PUT or DELETE can repair
the record; any attempt to save a newly invalid CORS document remains rejected.

## Deterministic Ordering

States use the following total order:

```text
1. SourceUpdatedAt
2. Kind: baseline < live < tombstone
3. For live/live ties: lexicographic decoded payload bytes
```

The greater state wins.

Consequences:

- a newer source event wins regardless of arrival order;
- the same payload with a newer timestamp is a greater state and advances the
  ordering barrier;
- a DELETE wins an equal-timestamp PUT/DELETE conflict;
- two equal-timestamp live payloads choose the same bytewise winner at every
  site;
- an exact duplicate is equal and therefore a no-op;
- retry, reordering, and duplicate delivery cannot move local state backward.

The live-payload tie-breaker is not intended to identify the human's temporal
intent. It supplies the deterministic result required when the timestamp has
already failed to distinguish two writes.

## Why No Source-Site Tie-Breaker

The rejected source-site alternative ordered states by timestamp plus origin
deployment ID. It would require a new origin field in madmin-go transport and
a persisted origin field in `BucketMetadata`. That adds a dependency release,
wire compatibility work, and an on-disk schema change without improving the
convergence guarantee over the content-based total order.

If a future product requirement needs provenance-aware conflict explanation,
the source-site design can be introduced as a versioned protocol. It is not
required to make the current register converge.

## Bucket Lineage and `CreatedAt`

`CreatedAt` protects a recreated bucket from delayed metadata events belonging
to the prior bucket incarnation:

```text
if incoming.SourceUpdatedAt < local.CreatedAt:
    ignore and log once per bucket
```

The floor is retained because removing it could install an old CORS grant on a
new bucket with the same name. It is intentionally not used to classify the
baseline.

For current-version site replication, local events are generated strictly
after `max(CreatedAt, current CORS barrier)`, and bucket creation timestamps are
propagated before initial metadata sync. A floor rejection therefore indicates
a stale lineage event, clock/history corruption, or a mixed/unsupported setup.
The rejection is observable through a bucket-scoped log-once message and the
remaining status mismatch.

## Local Transition

PUT and DELETE use the same CORS-specific transition helper.

Under the bucket CORS namespace lock:

1. load the current `.metadata.bin` through the migration-aware parsed loader;
2. validate the new live payload, if any;
3. choose:

   ```text
   UpdatedAt = max(UTCNow, CreatedAt + epsilon, CurrentBarrier + epsilon)
   ```

4. store either the live bytes or a nil tombstone with that timestamp;
5. save and refresh the parsed cache; and
6. release the lock before invoking `BucketMetaHook`.

This preserves HTTP semantics while ensuring a local administrative action is
strictly greater than the state it observed, including a future-dated peer
barrier caused by clock skew. The peer path deliberately uses a raw metadata
read instead: it must preserve the exact zero baseline and reject missing
metadata rather than implicitly creating a peer bucket record.

## Peer and Legacy-Bulk Transition

Typed CORS dispatch decodes and validates the payload, then performs this join
under the same lock:

```text
if incoming timestamp is zero:
    reject
if incoming timestamp is before CreatedAt:
    ignore and log
if incoming state <= local state:
    no-op
otherwise:
    persist incoming payload and exact source timestamp
```

The admin handler's legacy/default bulk metadata path can also carry a non-nil
CORS field. It therefore takes the shared metadata lock, applies strict
decoding and validation, and uses the same state comparison before saving. A
nil CORS field in that untyped legacy shape means "not included" and cannot
represent a tombstone; current producers use the typed CORS event for deletion.

## Concurrency and Locking

The transition lock is:

```text
.minio.sys / buckets/<bucket>/metadata.lock
```

It is a virtual distributed namespace lock. The name deliberately differs from
the real `buckets/<bucket>/.metadata.bin` object because the metadata save path
locks that object internally and namespace locks are not re-entrant.

The lock serializes CORS transitions with ordinary `Update`/`Delete`, legacy
bulk metadata, imports, bucket creation/adoption, and metadata migrations.
Every whole-record writer reads the latest disk state while holding the same
lock, so a writer for another configuration type cannot restore stale CORS
columns. Per-type validation, timestamp, and deletion semantics remain
independent.

No cross-site admin call or `BucketMetaHook` dispatch is made while holding the
metadata lock. The local disk save and resident-cache update complete under the
lock; intra-cluster metadata reload fan-out happens only after release. This
avoids a peer reload that needs migration from waiting on a lock held by the
notifying node. Reordered cross-site delivery is handled by the total-order
join.

The lock name changes from `cors-config.lock` to `metadata.lock`. During a
rolling upgrade, old and new nodes therefore do not serialize metadata writers
with each other; the shared-lock guarantee begins only after every node in the
cluster runs the new binary. Operators should avoid bucket-metadata changes
during that window. The on-disk record is unchanged, so rollback remains
format-compatible.

## Dispatch and Retry

PUT sends a typed `SRBucketMetaTypeCorsConfig` event with canonical base64 XML
and the local source timestamp. DELETE sends the same type with `Cors == nil`
and the tombstone timestamp.

`BucketMetaHook` may deliver concurrently to sites, fail on a subset, or be
retried by an external operation. The receiver transition is idempotent, so the
transport does not need to impose a global event order.

Current-version admin dispatch routes the typed event directly to
`PeerBucketCorsConfigHandler`. The legacy/default path is hardened only to
prevent a non-nil CORS field from bypassing the join; it is not a tombstone
compatibility protocol.

## Status Projection

`SiteReplicationMetaInfo` always exports `CorsConfigUpdatedAt`, including zero
baseline and nil tombstone states. It exports `CorsConfig` only for a live
payload.

Status considers sites converged if and only if every site has the same full
CORS state:

```text
(kind, decoded payload bytes, SourceUpdatedAt)
```

Live payload counts remain useful for per-site summary totals, but they do not
determine `CorsCfgMismatch`.

Examples:

| Site A | Site B | Mismatch |
| --- | --- | --- |
| baseline | baseline | no |
| same live bytes at same timestamp | same live bytes at same timestamp | no |
| same live bytes at different timestamps | same live bytes at different timestamps | yes |
| same tombstone timestamp | same tombstone timestamp | no |
| tombstones at different timestamps | tombstones at different timestamps | yes |
| live | tombstone | yes |
| invalid wire state | any state | yes |

## Winner Selection and Heal

Heal computes the maximum non-baseline valid state using the total order.
Selection is independent of Go map iteration. Deployment ID is used only as a
stable log-source choice when two sites already expose exactly equal states.

For each different site:

- the local site delegates to the normal peer CORS transition, preserving the
  source timestamp and lock discipline;
- a remote site receives a typed `SRBucketMetaTypeCorsConfig` event with the
  winner's canonical payload or nil tombstone and exact timestamp.

Payload equality alone is insufficient. A site with identical bytes at an
older timestamp is healed so it acquires the same future ordering barrier.

If every reported state is baseline, there is no event to propagate. If every
reported state is invalid, status remains mismatched and heal does not select
corrupt input as a source.

## Initial Sync

Initial sync emits:

- a live event when `CorsConfigUpdatedAt` is non-zero and payload is live;
- a tombstone event when `CorsConfigUpdatedAt` is non-zero and payload is nil;
- no event for the zero baseline.

Replaying initial sync is idempotent. A missed DELETE is recoverable because
the tombstone is part of the snapshot rather than being inferred from the
absence of a live payload.

All sites must run the CORS-aware implementation before enabling or mutating
per-bucket CORS. An older receiver can route an unknown typed event through a
legacy path that cannot represent deletion and does not provide this ordering
contract.

## Persistence and Restart

`CorsConfigXML` and `CorsConfigUpdatedAt` are persisted together in
`BucketMetadata` msgpack. Zero time round-trips as zero; CORS is deliberately
not defaulted to `CreatedAt` during load.

`BucketMetadata.Save` parses the live CORS XML before writing and before the
metadata system replaces the local cache. Therefore a rejected payload cannot
poison disk or cache, and a successful peer/heal transition immediately serves
the newly persisted parsed configuration.

After cache removal or process restart:

- a live state restores the same parsed rules and source timestamp;
- a tombstone restores nil payload plus its non-zero timestamp;
- a baseline remains nil plus zero timestamp.
- a legacy-invalid raw document leaves the non-CORS bucket metadata readable,
  disables per-bucket CORS fail-closed, and remains repairable through a valid
  CORS PUT or DELETE.

## Error Handling

| Error | Behavior |
| --- | --- |
| zero source timestamp on a peer live/delete event | reject the event |
| invalid/non-canonical base64 | reject before locking or saving |
| empty non-nil payload | reject |
| malformed XML | reject before saving |
| semantically invalid CORS rules | reject before saving |
| legacy-invalid CORS already on disk | load other metadata, return a CORS-specific error, and permit CORS replacement or deletion |
| event before bucket `CreatedAt` | ignore and log once per bucket |
| missing bucket metadata | return an error; do not create metadata implicitly |
| exact duplicate or lower state | successful no-op |
| remote heal failure | log the peer error; future heal cycles retry |

## Alternatives Considered

### Timestamp only

Rejected. Ignoring or accepting every equal-timestamp conflict leaves an
already divergent pair without a deterministic repair rule.

### Timestamp plus source deployment ID

Rejected for the current protocol. It is convergent, but requires madmin-go,
wire, and persisted-schema changes without improving convergence over the
selected total order.

### Payload-only status and heal

Rejected. It cannot distinguish ordering barriers and suppresses the exact
heal needed to make later event acceptance consistent.

### Default baseline timestamp to bucket creation

Rejected. It conflates baseline classification with a mutable value that may
come from a different snapshot and can turn a never-configured site into a
false tombstone source.

### Reuse `.metadata.bin` as the transition lock

Rejected. The save path takes the same namespace lock internally; reusing it
would self-deadlock.

### Give every metadata type a new state machine

Rejected. The shared lock prevents whole-record lost updates without changing
the independent replication, validation, or deletion semantics of policy,
tags, SSE, quota, versioning, and Object Lock. Those semantic audits remain
separate from the persistence fix in issue #102.

## Invariants

The implementation is acceptable only while all of these invariants hold:

1. Zero timestamp plus nil payload is the only baseline representation.
2. Nil payload plus non-zero timestamp is a durable tombstone.
3. A live payload has canonical base64 on the wire, valid CORS XML, and a
   non-zero source timestamp.
4. Every intentional current-version CORS state transition and every other
   whole-record metadata writer is serialized by `metadata.lock` from the
   authoritative disk read through save and local cache publication.
5. Peer apply and heal never replace local state with a lower or equal state.
6. Local PUT and DELETE create a state strictly greater than the state observed
   under the lock.
7. A peer event before local bucket creation cannot modify the new bucket
   lineage.
8. Status reports convergence only for identical full states.
9. Heal selects the same maximum regardless of arrival order, site, or map
   iteration order.
10. Same-payload/newer-timestamp heal advances the older barrier.
11. Initial sync and retry preserve tombstones and source timestamps.
12. Disk reload and cache reload preserve state kind, payload, and timestamp.
13. A legacy-invalid CORS document cannot activate global fallback, hide other
    bucket metadata, or prevent a valid CORS PUT/DELETE repair.

## Test Contract

The required test matrix is:

| Area | Required evidence |
| --- | --- |
| Wire | canonical base64 accepted; case-different decoded bytes differ; malformed and non-canonical base64 rejected |
| Validation | invalid XML and semantically invalid origin/method/rule rejected without mutation |
| Strict wire | standard S3 namespace accepted; trailing root, unknown/nested elements, duplicate singleton fields, lowercase methods, byte-counted Unicode IDs, and invalid MaxAge rejected |
| Ordering | older event ignored; newer event applied; duplicate no-op; equal live/live order-independent; equal PUT/DELETE chooses tombstone |
| Barrier | same payload with newer timestamp is persisted and healed |
| Tombstone | delayed PUT cannot resurrect; missed DELETE wins heal; repeated DELETE is idempotent |
| Status | baseline, live, tombstone, payload mismatch, and timestamp-only mismatch classified correctly |
| Winner | three-site equal-timestamp selection remains deterministic across repeated map iteration |
| Concurrency | concurrent peer and legacy-bulk events converge to the total-order maximum |
| Local concurrency | concurrent local PUT/DELETE timestamps are unique and final state matches the last serialized transition |
| Initial sync | baseline omitted; live and tombstone emitted with exact source timestamp |
| Lineage | pre-creation event ignored; post-creation event applied |
| Restart | cache removal/disk reload preserves tombstone or live state and status timestamp |
| Legacy repair | a lenient historical document loads fail-closed without hiding other metadata and can be deleted or replaced |
| Full seam | signed admin dispatch -> peer apply -> real status collection -> local heal -> cache reload -> remote heal dispatch |

## Local Verification Record

The committed B2 implementation passed:

- the supplied adversarial base64 and same-payload/newer-timestamp tests;
- focused CORS normal tests;
- focused CORS race tests;
- `go test ./internal/bucket/cors` and its race run;
- `go test ./cmd -count=1`;
- `go vet ./...`;
- `go build ./...`;
- repository-configured golangci-lint v2.13.1 with zero issues;
- gofmt and `git diff --check`;
- a signed admin dispatch -> apply -> status -> heal -> cache reload test.

After integrating B3 and resolving overlap, the frozen combination passed:

- focused strict-parser, validation, middleware, replication, namespace,
  legacy-repair, and race tests;
- CI-tagged `go test ./...`, full vet/build, module verification, pinned lint,
  and rebrand/compatibility checks;
- a real local two-site deployment with two nodes per site, including
  bidirectional replacement, a site missing DELETE while offline, restart
  heal, and a second restart preserving the tombstone; and
- raw SigV4 wire probes that reject a lowercase method and trailing XML root,
  accept a 255-code-point Unicode ID, and replicate the accepted config.

The public EN/ZH design records pass a warning-fatal Hugo build, rendered link
checking, and local browser QA. These results are acceptance evidence, not a
release, deployment, tag, or production claim.

The repository `make lint` bootstrap could not download its private copy of
golangci-lint because the network returned HTTP status 000. The same exact
v2.13.1 binary already installed locally was used with the Makefile's build
tags, timeout, and configuration and reported zero issues.

## Independent Review Record

Four read-only local Claude Code reviews used canonical model
`claude-opus-5` at `max` effort.

The first review rejected the pre-fix candidate and identified the unsafe
CreatedAt-based baseline, missing deterministic tie-break, missing atomic join,
non-monotonic local barrier, timestamp-blind status, and initial-sync tombstone
gap. The selected C-prime model incorporated the valid findings while rejecting
the suggestion to rewrite normal source timestamps.

The second review found no P0. Its `GO WITH FIXES` findings were peer semantic
validation, the legacy/default admin mutation path bypassing the then-current
CORS lock and join, CreatedAt-floor observability, and missing tests for
invalid XML, lineage, and concurrent local transitions. Those required changes
and tests are now in the working tree.

The final review examined this design and the exact dirty diff, independently
reran build, vet, lint, normal tests, and race tests, and found no P0 or P1.
Its verdict was `GO WITH FIXES`: the implementation was explicitly judged GO,
while five design-document statements required correction. It also suggested
an optional status hardening so semantically invalid canonical payloads are
not selected and retransmitted. The hardening and all mandatory documentation
corrections are incorporated in the current tree. The final selected solution
is therefore the C-prime register and invariants recorded in this document.

The fourth review examined the resolved B2+B3 combination. It confirmed that
the C-prime register, strict wire parser, MaxAge presence, wildcard credentials,
Origin-null marker, rejected-preflight `Vary`, checksum classification, and
peer validation can coexist. It found a conflict-resolution test helper typo
and the risk that strict parsing could make all bucket metadata unavailable for
a document accepted by a lenient development build. The helper was corrected;
metadata loading now stashes a CORS-specific error, fails browser behavior
closed, rejects new invalid saves, and allows a valid CORS PUT/DELETE repair.

## Release Gates

An implementation-level GO means only that the local CORS state machine and
tests satisfy this document. It does not authorize a release.

Before closing issue #75 or publishing a server artifact:

1. commit the exact reviewed implementation and design with DCO sign-off;
2. push a focused branch and run remote PR CI;
3. merge and confirm main CI on the merge commit;
4. finish public EN/ZH upgrade, fallback, and downgrade documentation in
   `silo.pgsty.com`;
5. run a real two-site process test for PUT, DELETE, simultaneous conflict,
   offline peer restart, status, and heal;
6. verify no release tag or image contains an intermediate candidate; and
7. treat package, image, SBOM, signature, canary, and production verification
   as separate gates.
