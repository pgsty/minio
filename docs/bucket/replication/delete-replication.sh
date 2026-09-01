#!/usr/bin/env bash

echo "Running $0"

if [ -n "$TEST_DEBUG" ]; then
	set -x
fi

trap 'catch $LINENO' ERR

# shellcheck disable=SC2120
catch() {
	if [ $# -ne 0 ]; then
		echo "error on line $1"
		echo "dc1 server logs ========="
		cat /tmp/dc1.log
		echo "dc2 server logs ========="
		cat /tmp/dc2.log
	fi

	echo "Cleaning up instances of Silo"
	set +e
	pkill silo
	pkill mc
	rm -rf /tmp/xl/
	if [ $# -ne 0 ]; then
		exit $#
	fi
}

catch

set -e
export MINIO_CI_CD=1
export MINIO_BROWSER=off
export MINIO_ROOT_USER="minio"
export MINIO_ROOT_PASSWORD="silo123"
export MINIO_KMS_AUTO_ENCRYPTION=off
export MINIO_PROMETHEUS_AUTH_TYPE=public
export MINIO_KMS_SECRET_KEY=my-minio-key:OSMM+vkKUTCvQs9YL/CVMIMt43HFhkUpqJxTmGl6rYw=
unset MINIO_KMS_KES_CERT_FILE
unset MINIO_KMS_KES_KEY_FILE
unset MINIO_KMS_KES_ENDPOINT
unset MINIO_KMS_KES_KEY_NAME

if [ ! -f ./mc ]; then
	"$(git rev-parse --show-toplevel)/buildscripts/install-mcli.sh" ./mc
fi

mkdir -p /tmp/xl/1/ /tmp/xl/2/

export MINIO_KMS_SECRET_KEY="my-minio-key:OSMM+vkKUTCvQs9YL/CVMIMt43HFhkUpqJxTmGl6rYw="
export MINIO_ROOT_USER="minioadmin"
export MINIO_ROOT_PASSWORD="minioadmin"

./silo server --address ":9001" /tmp/xl/1/{1...4}/ 2>&1 >/tmp/dc1.log &
pid1=$!
./silo server --address ":9002" /tmp/xl/2/{1...4}/ 2>&1 >/tmp/dc2.log &
pid2=$!

sleep 3

export MC_HOST_mysilo1=http://minioadmin:minioadmin@localhost:9001
export MC_HOST_mysilo2=http://minioadmin:minioadmin@localhost:9002

./mc ready mysilo1
./mc ready mysilo2

./mc mb mysilo1/testbucket/
./mc version enable mysilo1/testbucket/
./mc mb mysilo2/testbucket/
./mc version enable mysilo2/testbucket/

./mc replicate add mysilo1/testbucket --remote-bucket http://minioadmin:minioadmin@localhost:9002/testbucket/ --priority 1

# Test replication of delete markers and permanent deletes

./mc cp README.md mysilo1/testbucket/dir/file
./mc cp README.md mysilo1/testbucket/dir/file

sleep 1s

echo "=== mysilo1"
./mc ls --versions mysilo1/testbucket/dir/file

echo "=== mysilo2"
./mc ls --versions mysilo2/testbucket/dir/file

versionId="$(./mc ls --json --versions mysilo1/testbucket/dir/ | tail -n1 | jq -r .versionId)"

./mc rm --version-id "$versionId" mysilo1/testbucket/dir/file

./mc ls -r --versions mysilo1/testbucket >/tmp/mysilo1.txt
./mc ls -r --versions mysilo2/testbucket >/tmp/mysilo2.txt

out=$(diff -qpruN /tmp/mysilo1.txt /tmp/mysilo2.txt)
ret=$?
if [ $ret -ne 0 ]; then
	echo "BUG: expected no missing entries after replication: $out"
	exit 1
fi

./mc rm mysilo1/testbucket/dir/file
sleep 1s

./mc ls -r --versions mysilo1/testbucket >/tmp/mysilo1.txt
./mc ls -r --versions mysilo2/testbucket >/tmp/mysilo2.txt

out=$(diff -qpruN /tmp/mysilo1.txt /tmp/mysilo2.txt)
ret=$?
if [ $ret -ne 0 ]; then
	echo "BUG: expected no missing entries after replication: $out"
	exit 1
fi

# Verify the documented least-privilege target policy. Explicit version
# deletion on the receiver is replication traffic, so the target credential
# needs DeleteObject + ReplicateDelete but not DeleteObjectVersion.
./mc mb mysilo1/leastpriv/ mysilo2/leastpriv/ --with-versioning
./mc admin user add mysilo2 repluser repluser123
cat >/tmp/xl/replpolicy.json <<'EOF'
{
 "Version": "2012-10-17",
 "Statement": [
  {
   "Effect": "Allow",
   "Action": [
    "s3:GetReplicationConfiguration",
    "s3:ListBucket",
    "s3:ListBucketMultipartUploads",
    "s3:GetBucketLocation",
    "s3:GetBucketVersioning"
   ],
   "Resource": ["arn:aws:s3:::leastpriv"]
  },
  {
   "Effect": "Allow",
   "Action": [
    "s3:GetReplicationConfiguration",
    "s3:ReplicateTags",
    "s3:AbortMultipartUpload",
    "s3:GetObject",
    "s3:GetObjectVersion",
    "s3:GetObjectVersionTagging",
    "s3:PutObject",
    "s3:DeleteObject",
    "s3:ReplicateObject",
    "s3:ReplicateDelete"
   ],
   "Resource": ["arn:aws:s3:::leastpriv/*"]
  }
 ]
}
EOF
./mc admin policy create mysilo2 replpolicy /tmp/xl/replpolicy.json
./mc admin policy attach mysilo2 replpolicy --user repluser
./mc replicate add mysilo1/leastpriv --remote-bucket http://repluser:repluser123@localhost:9002/leastpriv/ --priority 1 --replicate delete,delete-marker

./mc cp README.md mysilo1/leastpriv/dir/file
./mc cp README.md mysilo1/leastpriv/dir/file
sleep 1s

leastPrivVersionId="$(./mc ls --json --versions mysilo1/leastpriv/dir/ | tail -n1 | jq -r .versionId)"
./mc rm --version-id "$leastPrivVersionId" mysilo1/leastpriv/dir/file
sleep 1s
./mc ls -r --versions mysilo1/leastpriv >/tmp/leastpriv1.txt
./mc ls -r --versions mysilo2/leastpriv >/tmp/leastpriv2.txt
out=$(diff -qpruN /tmp/leastpriv1.txt /tmp/leastpriv2.txt)
ret=$?
if [ $ret -ne 0 ]; then
	echo "BUG: least-privilege version delete did not replicate: $out"
	exit 1
fi

./mc rm mysilo1/leastpriv/dir/file
sleep 1s
./mc ls -r --versions mysilo1/leastpriv >/tmp/leastpriv1.txt
./mc ls -r --versions mysilo2/leastpriv >/tmp/leastpriv2.txt
out=$(diff -qpruN /tmp/leastpriv1.txt /tmp/leastpriv2.txt)
ret=$?
if [ $ret -ne 0 ]; then
	echo "BUG: least-privilege delete marker did not replicate: $out"
	exit 1
fi

# Test listing of non replicated permanent deletes

set -x

./mc mb mysilo1/foobucket/ mysilo2/foobucket/ --with-versioning
./mc replicate add mysilo1/foobucket --remote-bucket http://minioadmin:minioadmin@localhost:9002/foobucket/ --priority 1
./mc cp README.md mysilo1/foobucket/dir/file

versionId="$(./mc ls --json --versions mysilo1/foobucket/dir/ | jq -r .versionId)"

kill ${pid2} && wait ${pid2} || true

./mc rm --version-id "$versionId" mysilo1/foobucket/dir/file

out="$(./mc ls mysilo1/foobucket/dir/)"
if [ "$out" != "" ]; then
	echo "BUG: non versioned listing should not show pending/failed replicated delete:"
	echo "$out"
	exit 1
fi

out="$(./mc ls --versions mysilo1/foobucket/dir/)"
if [ "$out" != "" ]; then
	echo "BUG: versioned listing should not show pending/failed replicated deletes:"
	echo "$out"
	exit 1
fi

echo "Success"
catch
