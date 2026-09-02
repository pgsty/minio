#!/bin/bash -e
#

set -E
set -o pipefail

if [ ! -x "$PWD/silo" ]; then
	echo "Silo executable binary not found in current directory"
	exit 1
fi

WORK_DIR="$PWD/.verify-$RANDOM"
SILO_CONFIG_DIR="$WORK_DIR/.silo"
SILO=("$PWD/silo" --config-dir "$SILO_CONFIG_DIR" server)

function start_silo_3_node() {
	export MINIO_ROOT_USER=silo
	export MINIO_ROOT_PASSWORD=silo1234
	export MINIO_ERASURE_SET_DRIVE_COUNT=6
	export MINIO_CI_CD=1

	start_port=$1
	args=""
	for i in $(seq 1 3); do
		args="$args http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/1/ http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/2/ http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/3/ http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/4/ http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/5/ http://127.0.0.1:$((start_port + i))${WORK_DIR}/$i/6/"
	done

	"${SILO[@]}" --address ":$((start_port + 1))" $args >"${WORK_DIR}/dist-silo-server1.log" 2>&1 &
	pid1=$!
	disown ${pid1}

	"${SILO[@]}" --address ":$((start_port + 2))" $args >"${WORK_DIR}/dist-silo-server2.log" 2>&1 &
	pid2=$!
	disown $pid2

	"${SILO[@]}" --address ":$((start_port + 3))" $args >"${WORK_DIR}/dist-silo-server3.log" 2>&1 &
	pid3=$!
	disown $pid3

	export MC_HOST_mysilo="http://silo:silo1234@127.0.0.1:$((start_port + 1))"

	timeout 15m /tmp/mc ready mysilo || fail

	# Wait for all drives to be online and formatted
	while [ $(/tmp/mc admin info --json mysilo | jq '.info.servers[].drives[].state | select(. != "ok")' | wc -l) -gt 0 ]; do sleep 1; done
	# Wait for all drives to be healed
	while [ $(/tmp/mc admin info --json mysilo | jq '.info.servers[].drives[].healing | select(. != null) | select(. == true)' | wc -l) -gt 0 ]; do sleep 1; done

	# Wait for Status: in MinIO output
	while true; do
		rv=$(check_online)
		if [ "$rv" != "1" ]; then
			# success
			break
		fi

		# Check if we should retry
		retry=$((retry + 1))
		if [ $retry -le 20 ]; then
			sleep 5
			continue
		fi

		# Failure
		fail
	done

	if ! ps -p $pid1 1>&2 >/dev/null; then
		echo "silo-server-1 is not running." && fail
	fi

	if ! ps -p $pid2 1>&2 >/dev/null; then
		echo "silo-server-2 is not running." && fail
	fi

	if ! ps -p $pid3 1>&2 >/dev/null; then
		echo "silo-server-3 is not running." && fail
	fi

	if ! pkill silo; then
		fail
	fi

	sleep 1
	if pgrep silo; then
		# forcibly killing, to proceed further properly.
		if ! pkill -9 silo; then
			echo "no Silo process running anymore, proceed."
		fi
	fi
}

function fail() {
	for i in $(seq 1 3); do
		echo "server$i log:"
		cat "${WORK_DIR}/dist-silo-server$i.log"
	done
	echo "FAILED"
	purge "$WORK_DIR"
	exit 1
}

function check_online() {
	if ! grep -q 'API:' ${WORK_DIR}/dist-silo-*.log; then
		echo "1"
	fi
}

function purge() {
	echo rm -rf "$1"
}

function __init__() {
	echo "Initializing environment"
	mkdir -p "$WORK_DIR"
	mkdir -p "$SILO_CONFIG_DIR"

	## version is purposefully set to '3' for minio to migrate configuration file
	echo '{"version": "3", "credential": {"accessKey": "silo", "secretKey": "silo1234"}, "region": "us-east-1"}' >"$SILO_CONFIG_DIR/config.json"

	if [ ! -f /tmp/mc ]; then
		"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" /tmp/mc
	fi
}

function perform_test() {
	start_silo_3_node $2

	echo "Testing Distributed Erasure setup healing of drives"
	echo "Remove the contents of the disks belonging to '${1}' erasure set"

	rm -rf ${WORK_DIR}/${1}/*/

	set -x
	start_silo_3_node $2
}

function main() {
	# use same ports for all tests
	start_port=$(shuf -i 10000-65000 -n 1)

	perform_test "2" ${start_port}
	perform_test "1" ${start_port}
	perform_test "3" ${start_port}
}

(__init__ "$@" && main "$@")
rv=$?
purge "$WORK_DIR"
exit "$rv"
