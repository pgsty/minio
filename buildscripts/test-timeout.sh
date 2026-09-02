#!/bin/bash

if [ -n "$TEST_DEBUG" ]; then
	set -x
fi

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

trap 'catch $LINENO' ERR

function purge() {
	rm -rf "$1"
}

# shellcheck disable=SC2120
catch() {
	if [ $# -ne 0 ]; then
		echo "error on line $1"
	fi

	echo "Cleaning up instances of Silo"
	pkill silo || true
	pkill -9 silo || true
	purge "$WORK_DIR"
	if [ $# -ne 0 ]; then
		exit $#
	fi
}

catch

function gen_put_request() {
	hdr_sleep=$1
	body_sleep=$2

	echo "PUT /testbucket/testobject HTTP/1.1"
	sleep $hdr_sleep
	echo "Host: foo-header"
	echo "User-Agent: curl/8.2.1"
	echo "Accept: */*"
	echo "Content-Length: 30"
	echo ""

	sleep $body_sleep
	echo "random line 0"
	echo "random line 1"
	echo ""
	echo ""
}

function send_put_object_request() {
	hdr_timeout=$1
	body_timeout=$2

	start=$(date +%s)
	timeout 5m bash -c "gen_put_request $hdr_timeout $body_timeout | netcat 127.0.0.1 $start_port | read" || return -1
	[ $(($(date +%s) - start)) -gt $((srv_hdr_timeout + srv_idle_timeout + 1)) ] && return -1
	return 0
}

function test_silo_with_timeout() {
	start_port=$1

	export MINIO_ROOT_USER=silo
	export MINIO_ROOT_PASSWORD=silo1234
	export MC_HOST_silo="http://silo:silo1234@127.0.0.1:${start_port}/"
	export MINIO_CI_CD=1

	mkdir ${WORK_DIR}
	if [ ! -x "$PWD/mc" ]; then
		"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" "$PWD/mc"
	fi

	"${SILO[@]}" --address ":$start_port" --read-header-timeout ${srv_hdr_timeout}s --idle-timeout ${srv_idle_timeout}s "${WORK_DIR}/disk/" >"${WORK_DIR}/server1.log" 2>&1 &
	pid=$!
	disown $pid
	sleep 1

	if ! ps -p ${pid} 1>&2 >/dev/null; then
		echo "server1 log:"
		cat "${WORK_DIR}/server1.log"
		echo "FAILED"
		purge "$WORK_DIR"
		exit 1
	fi

	set -e

	"${PWD}/mc" mb silo/testbucket
	"${PWD}/mc" anonymous set public silo/testbucket

	# slow header writing
	send_put_object_request 20 0 && exit -1
	"${PWD}/mc" stat silo/testbucket/testobject && exit -1

	# quick header write and slow bodywrite
	send_put_object_request 0 40 && exit -1
	"${PWD}/mc" stat silo/testbucket/testobject && exit -1

	# quick header and body write
	send_put_object_request 1 1 || exit -1
	"${PWD}/mc" stat silo/testbucket/testobject || exit -1
}

function main() {
	export start_port=$(shuf -i 10000-65000 -n 1)
	export srv_hdr_timeout=5
	export srv_idle_timeout=5
	export -f gen_put_request

	test_silo_with_timeout ${start_port}
}

main "$@"
