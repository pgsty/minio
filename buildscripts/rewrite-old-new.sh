#!/bin/bash -e

set -E
set -o pipefail
set -x

WORK_DIR="$PWD/.verify-$RANDOM"
SILO_CONFIG_DIR="$WORK_DIR/.silo"
MINIO_OLD=("$PWD/minio.RELEASE.2020-10-28T08-16-50Z" --config-dir "$SILO_CONFIG_DIR" server)
SILO=("$PWD/silo" --config-dir "$SILO_CONFIG_DIR" server)

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

function download_old_release() {
	if [ ! -x minio.RELEASE.2020-10-28T08-16-50Z ]; then
		: "${SILO_LEGACY_FIXTURE_2020:?set SILO_LEGACY_FIXTURE_2020 to the audited legacy binary}"
		: "${SILO_LEGACY_SHA256_2020:?set SILO_LEGACY_SHA256_2020 to its audited SHA-256}"
		"$(git rev-parse --show-toplevel)/buildscripts/install-verified-fixture.sh" \
			"${SILO_LEGACY_FIXTURE_2020}" "${SILO_LEGACY_SHA256_2020}" \
			"minio.RELEASE.2020-10-28T08-16-50Z"
	fi
}

function verify_rewrite() {
	start_port=$1

	export MINIO_ACCESS_KEY=silo
	export MINIO_SECRET_KEY=silo1234
	export MC_HOST_silo="http://silo:silo1234@127.0.0.1:${start_port}/"
	unset MINIO_KMS_AUTO_ENCRYPTION # do not auto-encrypt objects
	export MINIO_CI_CD=1

	mkdir -p "${WORK_DIR}"
	"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" "${WORK_DIR}/mc"

	"${MINIO_OLD[@]}" --address ":$start_port" "${WORK_DIR}/xl{1...16}" >"${WORK_DIR}/server1.log" 2>&1 &
	pid=$!
	disown $pid

	"${WORK_DIR}/mc" ready silo/

	if ! ps -p ${pid} 1>&2 >/dev/null; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	"${WORK_DIR}/mc" mb silo/healing-rewrite-bucket --quiet --with-lock
	"${WORK_DIR}/mc" cp \
		buildscripts/verify-build.sh \
		silo/healing-rewrite-bucket/ \
		--disable-multipart --quiet

	"${WORK_DIR}/mc" cp \
		buildscripts/verify-build.sh \
		silo/healing-rewrite-bucket/ \
		--disable-multipart --quiet

	"${WORK_DIR}/mc" cp \
		buildscripts/verify-build.sh \
		silo/healing-rewrite-bucket/ \
		--disable-multipart --quiet

	kill ${pid}
	sleep 3

	"${SILO[@]}" --address ":$start_port" "${WORK_DIR}/xl{1...16}" >"${WORK_DIR}/server1.log" 2>&1 &
	pid=$!
	disown $pid

	"${WORK_DIR}/mc" ready silo/

	if ! ps -p ${pid} 1>&2 >/dev/null; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	if ! ./s3-check-md5 \
		-debug \
		-versions \
		-access-key silo \
		-secret-key silo1234 \
		-endpoint "http://127.0.0.1:${start_port}/" 2>&1 | grep INTACT; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		mkdir -p inspects
		(
			cd inspects
			"${WORK_DIR}/mc" admin inspect silo/healing-rewrite-bucket/verify-build.sh/**
		)

		"${WORK_DIR}/mc" mb play/inspects
		"${WORK_DIR}/mc" mirror inspects play/inspects

		purge "$WORK_DIR"
		exit 1
	fi

	go run ./buildscripts/heal-manual.go "127.0.0.1:${start_port}" "silo" "silo1234"
	sleep 1

	if ! ./s3-check-md5 \
		-debug \
		-versions \
		-access-key silo \
		-secret-key silo1234 \
		-endpoint http://127.0.0.1:${start_port}/ 2>&1 | grep INTACT; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		mkdir -p inspects
		(
			cd inspects
			"${WORK_DIR}/mc" admin inspect silo/healing-rewrite-bucket/verify-build.sh/**
		)

		"${WORK_DIR}/mc" mb play/inspects
		"${WORK_DIR}/mc" mirror inspects play/inspects

		purge "$WORK_DIR"
		exit 1
	fi

	kill ${pid}
}

function main() {
	download_old_release

	start_port=$(shuf -i 10000-65000 -n 1)

	verify_rewrite ${start_port}
}

function purge() {
	rm -rf "$1"
}

(main "$@")
rv=$?
purge "$WORK_DIR"
exit "$rv"
