#!/bin/bash
#

set -e
set -E
set -o pipefail

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

WORK_DIR="$PWD/.verify-$RANDOM"

export MINT_MODE=core
export MINT_DATA_DIR="$WORK_DIR/data"
export SERVER_ENDPOINT="127.0.0.1:9000"
export MC_HOST_verify="http://silo:silo1234@${SERVER_ENDPOINT}/"
export MC_HOST_verify_ipv6="http://silo:silo1234@[::1]:9000/"
export ACCESS_KEY="silo"
export SECRET_KEY="silo1234"
export ENABLE_HTTPS=0
export GO111MODULE=on
export GOGC=25
export ENABLE_ADMIN=1
export MINIO_CI_CD=1

SILO_CONFIG_DIR="$WORK_DIR/.silo"
SILO=("$PWD/silo" --config-dir "$SILO_CONFIG_DIR")

FILE_1_MB="$MINT_DATA_DIR/datafile-1-MB"
FILE_65_MB="$MINT_DATA_DIR/datafile-65-MB"

FUNCTIONAL_TESTS="$WORK_DIR/functional-tests.sh"

function start_silo_fs() {
	export MINIO_ROOT_USER=$ACCESS_KEY
	export MINIO_ROOT_PASSWORD=$SECRET_KEY
	"${SILO[@]}" server "${WORK_DIR}/fs-disk" >"$WORK_DIR/fs-silo.log" 2>&1 &

	"${WORK_DIR}/mc" ready verify
}

function start_silo_erasure() {
	"${SILO[@]}" server "${WORK_DIR}/erasure-disk1" "${WORK_DIR}/erasure-disk2" "${WORK_DIR}/erasure-disk3" "${WORK_DIR}/erasure-disk4" >"$WORK_DIR/erasure-silo.log" 2>&1 &

	"${WORK_DIR}/mc" ready verify
}

function start_silo_erasure_sets() {
	export MINIO_ENDPOINTS="${WORK_DIR}/erasure-disk-sets{1...32}"
	"${SILO[@]}" server >"$WORK_DIR/erasure-silo-sets.log" 2>&1 &

	"${WORK_DIR}/mc" ready verify
}

function start_silo_pool_erasure_sets() {
	export MINIO_ROOT_USER=$ACCESS_KEY
	export MINIO_ROOT_PASSWORD=$SECRET_KEY
	export MINIO_ENDPOINTS="http://127.0.0.1:9000${WORK_DIR}/pool-disk-sets{1...4} http://127.0.0.1:9001${WORK_DIR}/pool-disk-sets{5...8}"
	"${SILO[@]}" server --address ":9000" >"$WORK_DIR/pool-silo-9000.log" 2>&1 &
	"${SILO[@]}" server --address ":9001" >"$WORK_DIR/pool-silo-9001.log" 2>&1 &

	"${WORK_DIR}/mc" ready verify
}

function start_silo_pool_erasure_sets_ipv6() {
	export MINIO_ROOT_USER=$ACCESS_KEY
	export MINIO_ROOT_PASSWORD=$SECRET_KEY
	export MINIO_ENDPOINTS="http://[::1]:9000${WORK_DIR}/pool-disk-sets-ipv6{1...4} http://[::1]:9001${WORK_DIR}/pool-disk-sets-ipv6{5...8}"
	"${SILO[@]}" server --address="[::1]:9000" >"$WORK_DIR/pool-silo-ipv6-9000.log" 2>&1 &
	"${SILO[@]}" server --address="[::1]:9001" >"$WORK_DIR/pool-silo-ipv6-9001.log" 2>&1 &

	"${WORK_DIR}/mc" ready verify_ipv6
}

function start_silo_dist_erasure() {
	export MINIO_ROOT_USER=$ACCESS_KEY
	export MINIO_ROOT_PASSWORD=$SECRET_KEY
	export MINIO_ENDPOINTS="http://127.0.0.1:9000${WORK_DIR}/dist-disk1 http://127.0.0.1:9001${WORK_DIR}/dist-disk2 http://127.0.0.1:9002${WORK_DIR}/dist-disk3 http://127.0.0.1:9003${WORK_DIR}/dist-disk4"
	for i in $(seq 0 3); do
		"${SILO[@]}" server --address ":900${i}" >"$WORK_DIR/dist-silo-900${i}.log" 2>&1 &
	done

	"${WORK_DIR}/mc" ready verify
}

function run_test_fs() {
	start_silo_fs

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		cat "$WORK_DIR/fs-silo.log"
	fi
	rm -f "$WORK_DIR/fs-silo.log"

	return "$rv"
}

function run_test_erasure_sets() {
	start_silo_erasure_sets

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		cat "$WORK_DIR/erasure-silo-sets.log"
	fi
	rm -f "$WORK_DIR/erasure-silo-sets.log"

	return "$rv"
}

function run_test_pool_erasure_sets() {
	start_silo_pool_erasure_sets

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		for i in $(seq 0 1); do
			echo "server$i log:"
			cat "$WORK_DIR/pool-silo-900$i.log"
		done
	fi

	for i in $(seq 0 1); do
		rm -f "$WORK_DIR/pool-silo-900$i.log"
	done

	return "$rv"
}

function run_test_pool_erasure_sets_ipv6() {
	start_silo_pool_erasure_sets_ipv6

	export SERVER_ENDPOINT="[::1]:9000"

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		for i in $(seq 0 1); do
			echo "server$i log:"
			cat "$WORK_DIR/pool-silo-ipv6-900$i.log"
		done
	fi

	for i in $(seq 0 1); do
		rm -f "$WORK_DIR/pool-silo-ipv6-900$i.log"
	done

	return "$rv"
}

function run_test_erasure() {
	start_silo_erasure

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		cat "$WORK_DIR/erasure-silo.log"
	fi
	rm -f "$WORK_DIR/erasure-silo.log"

	return "$rv"
}

function run_test_dist_erasure() {
	start_silo_dist_erasure

	(cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
	rv=$?

	pkill silo
	sleep 3

	if [ "$rv" -ne 0 ]; then
		echo "server1 log:"
		cat "$WORK_DIR/dist-silo-9000.log"
		echo "server2 log:"
		cat "$WORK_DIR/dist-silo-9001.log"
		echo "server3 log:"
		cat "$WORK_DIR/dist-silo-9002.log"
		echo "server4 log:"
		cat "$WORK_DIR/dist-silo-9003.log"
	fi

	rm -f "$WORK_DIR/dist-silo-9000.log" "$WORK_DIR/dist-silo-9001.log" "$WORK_DIR/dist-silo-9002.log" "$WORK_DIR/dist-silo-9003.log"

	return "$rv"
}

function purge() {
	rm -rf "$1"
}

function __init__() {
	echo "Initializing environment"
	mkdir -p "$WORK_DIR"
	mkdir -p "$SILO_CONFIG_DIR"
	mkdir -p "$MINT_DATA_DIR"

	"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" "${WORK_DIR}/mc"

	shred -n 1 -s 1M - 1>"$FILE_1_MB" 2>/dev/null
	shred -n 1 -s 65M - 1>"$FILE_65_MB" 2>/dev/null

	## version is purposefully set to '3' for minio to migrate configuration file
	echo '{"version": "3", "credential": {"accessKey": "silo", "secretKey": "silo1234"}, "region": "us-east-1"}' >"$SILO_CONFIG_DIR/config.json"

	"$(git rev-parse --show-toplevel)/buildscripts/install-verified-fixture.sh" \
		https://raw.githubusercontent.com/pgsty/mc/4c4dcc4b55baf238cd0c81030d77945b3828f157/functional-tests.sh \
		9b98c8152b294d567b9bc732869226dd4f65d0f4d84092dc066a900a66a9e22c \
		"$FUNCTIONAL_TESTS"

	sed -i 's|-sS|-sSg|g' "$FUNCTIONAL_TESTS"
	chmod a+x "$FUNCTIONAL_TESTS"
}

function main() {
	echo "Testing in FS setup"
	if ! run_test_fs; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	echo "Testing in Erasure setup"
	if ! run_test_erasure; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	echo "Testing in Distributed Erasure setup"
	if ! run_test_dist_erasure; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	echo "Testing in Erasure setup as sets"
	if ! run_test_erasure_sets; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	echo "Testing in Distributed Eraure expanded setup"
	if ! run_test_pool_erasure_sets; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	echo "Testing in Distributed Erasure expanded setup with ipv6"
	if ! run_test_pool_erasure_sets_ipv6; then
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	purge "$WORK_DIR"
}

trap 'purge "$WORK_DIR"' EXIT
__init__ "$@"
main "$@"
