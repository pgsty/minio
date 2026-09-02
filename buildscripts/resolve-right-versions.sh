#!/bin/bash -e

set -E
set -o pipefail
set -x
set -e

WORK_DIR="$PWD/.verify-$RANDOM"
SILO_CONFIG_DIR="$WORK_DIR/.silo"
SILO=("$PWD/silo" --config-dir "$SILO_CONFIG_DIR" server)

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

function start_silo_5drive() {
	start_port=$1

	export MINIO_ROOT_USER=silo
	export MINIO_ROOT_PASSWORD=silo1234
	export MC_HOST_silo="http://silo:silo1234@127.0.0.1:${start_port}/"
	unset MINIO_KMS_AUTO_ENCRYPTION # do not auto-encrypt objects
	export MINIO_CI_CD=1

	mkdir -p "${WORK_DIR}"
	"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" "${WORK_DIR}/mc"

	"${WORK_DIR}/mc" cp --quiet -r "buildscripts/cicd-corpus/" "${WORK_DIR}/cicd-corpus/"

	"${SILO[@]}" --address ":$start_port" "${WORK_DIR}/cicd-corpus/disk{1...5}" >"${WORK_DIR}/server1.log" 2>&1 &
	pid=$!
	disown $pid
	sleep 5

	if ! ps -p ${pid} 1>&2 >/dev/null; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	"${WORK_DIR}/mc" stat silo/bucket/testobj

	pkill silo
	sleep 3
}

function main() {
	start_port=$(shuf -i 10000-65000 -n 1)

	start_silo_5drive ${start_port}
}

function purge() {
	rm -rf "$1"
}

(main "$@")
rv=$?
purge "$WORK_DIR"
exit "$rv"
