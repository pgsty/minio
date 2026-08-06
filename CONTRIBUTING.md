# Contributing to Silo

Silo welcomes focused contributions that improve security, reliability,
compatibility, packaging, tests, or maintainability. This repository preserves
MinIO-compatible interfaces and storage formats, so changes must identify and
test any compatibility impact.

## Development Workflow

Fork the current Silo source repository, create a topic branch, and submit a
pull request. Discuss broad or compatibility-sensitive changes in an issue
before implementation.

### Set up a checkout

```sh
git clone https://github.com/pgsty/silo
cd silo
go build -o silo .
./silo --version
```

### Keep the lineage remote separate

```sh
git remote add lineage https://github.com/minio/minio
git fetch lineage
```

Do not merge an upstream branch into a pull request unless the maintainers have
agreed on the scope. Silo intentionally carries a small downstream delta.

### Create your feature branch

Create a separate branch before making code changes:

```
git checkout -b my-new-feature
```

### Test Silo server changes

Before opening a pull request:

- Add or update tests for changed behavior.
- Run `make verifiers`.
- Run the smallest relevant package tests, then `make test` when practical.
- Run `make build` and confirm the generated executable is `silo`.
- Explain any preserved `MINIO_*`, `minio_*`, `x-minio-*`, `/minio/*`,
  `.minio.sys`, ARN, module/import-path, or serialized compatibility name.

### Commit changes

After verification, commit your changes with a concise message and a DCO
sign-off (see [Licensing of Contributions](#licensing-of-contributions)):

```
git commit -s -am 'Fix object replication retry handling'
```

### Push to the branch

Push your locally committed changes to the remote origin (your fork)

```
git push origin my-new-feature
```

### Create a Pull Request

Pull requests should include motivation, reproduction steps where applicable,
test evidence, compatibility notes, and documentation impact. Public product
documentation is owned by the separate
[`pgsty/silo.pgsty.com`](https://github.com/pgsty/silo.pgsty.com) repository.

## Licensing of Contributions

Silo is licensed under the [GNU AGPL v3.0 or later](LICENSE). Its core is
Copyright (c) MinIO, Inc.; the combined work can never be relicensed, and this
fork does not try to.

* **No CLA.** We do not ask you to sign a Contributor License Agreement and we
  do not take your copyright. Contributions are accepted inbound=outbound: you
  keep the copyright to your changes and license them under the same
  AGPL-3.0-or-later as the project itself. The maintainers receive no rights
  beyond the project license.

* **DCO sign-off required.** Every commit must carry a
  `Signed-off-by: Your Name <you@example.com>` trailer certifying the
  [Developer Certificate of Origin 1.1](https://developercertificate.org/) —
  your statement that you have the right to submit the code under the project
  license. Sign each commit with:

  ```
  git commit -s
  ```

  Forgot some? Repair your branch with `git rebase --signoff` and force-push.
  CI rejects pull requests containing unsigned commits; the sign-off email
  must match the commit author email. (Lowercase `-s` is the plain-text DCO
  sign-off; cryptographic `-S`/GPG signing is welcome but independent.)

* **Provenance.** Only submit code you are entitled to submit. This matters
  more here than in most projects: Silo carries a downstream delta over an
  upstream code base, and cherry-picks from the lineage remote or other forks
  are routine. When relaying a patch written by someone else, preserve original
  authorship (`git cherry-pick -x`, keep the author field and any existing
  `Signed-off-by` trailers) and add your own sign-off as the person passing it
  along. Never import code from a proprietary distribution.

* **File headers.** Files derived from upstream keep the original MinIO
  copyright header unchanged. New files added by this fork use the dual
  header, followed by the standard AGPL boilerplate:

  ```
  // Copyright (c) 2015-2025 MinIO, Inc.
  // Copyright (c) 2025-2026 PGSTY
  ```

* **Squash merges** must keep the `Signed-off-by:` trailers in the resulting
  commit message.

* **Authorship and tooling.** The human contributor is the author of the commit
  and the sole signatory of its DCO sign-off. Attribution trailers for
  assistive tooling (for example `Co-Authored-By:` naming an AI assistant) are
  informational only: they record which tools were used, and do not create
  authorship, co-authorship, or any copyright claim. Whoever signs off remains
  responsible for the content of the commit, whatever produced it.

## FAQs

### How does Silo manage dependencies?

Silo uses Go modules. Preserve the compatibility module and import paths in
`go.mod`; downstream forks are selected with explicit `replace` directives.

- Run `go get foo/bar` in the source folder to add the dependency to `go.mod` file.

To remove a dependency

- Edit your code and remove the import reference.
- Run `go mod tidy` in the source folder to remove dependency from `go.mod` file.

### What are the coding guidelines?

Follow the existing Go style, run `gofmt` on changed Go files, and keep changes
compact. See the Go project's [code review comments](https://go.dev/wiki/CodeReviewComments).
