#!/bin/bash -e

set -E
set -o pipefail
set -x

WORK_DIR="$PWD/.verify-$RANDOM"
SILO_CONFIG_DIR="$WORK_DIR/.silo"
SILO=("$PWD/silo" --config-dir "$SILO_CONFIG_DIR" server)

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

function start_silo_4drive() {
	start_port=$1

	export MINIO_ROOT_USER=silo
	export MINIO_ROOT_PASSWORD=silo1234
	export MC_HOST_silo="http://silo:silo1234@127.0.0.1:${start_port}/"
	unset MINIO_KMS_AUTO_ENCRYPTION # do not auto-encrypt objects
	export MINIO_CI_CD=1

	mkdir ${WORK_DIR}
	if [ ! -x "$PWD/mc" ]; then
		"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" "$PWD/mc"
	fi

	"${SILO[@]}" --address ":$start_port" "${WORK_DIR}/disk{1...4}" >"${WORK_DIR}/server1.log" 2>&1 &
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

	"${PWD}/mc" mb --with-versioning silo/bucket

	for i in $(seq 1 4); do
		"${PWD}/mc" cp /etc/hosts silo/bucket/testobj

		sudo chown -R root. "${WORK_DIR}/disk${i}"

		"${PWD}/mc" cp /etc/hosts silo/bucket/testobj

		sudo chown -R ${USER}. "${WORK_DIR}/disk${i}"
	done

	for vid in $("${PWD}/mc" ls --json --versions silo/bucket/testobj | jq -r .versionId); do
		"${PWD}/mc" cat --vid "${vid}" silo/bucket/testobj | md5sum
	done

	pkill silo
	sleep 3
}

function main() {
	start_port=$(shuf -i 10000-65000 -n 1)

	start_silo_4drive ${start_port}
}

function purge() {
	rm -rf "$1"
}

(main "$@")
rv=$?
purge "$WORK_DIR"
exit "$rv"
