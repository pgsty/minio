#!/usr/bin/env bash
set -euo pipefail

helm package helm/minio -d helm-releases/

helm repo index --merge index.yaml --url https://raw.githubusercontent.com/pgsty/minio/master .
