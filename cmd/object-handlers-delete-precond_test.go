// Copyright (c) 2015-2025 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"net/http"
	"testing"

	"github.com/minio/minio/internal/crypto"
)

// TestDeleteIfMatchPreconditionFailed exercises the pure If-Match evaluation for
// DeleteObject, including delete markers, the "*" wildcard, and SSE-C objects
// whose public ETag must be derived without the customer key.
func TestDeleteIfMatchPreconditionFailed(t *testing.T) {
	const plainETag = "d41d8cd98f00b204e9800998ecf8427e" // 32-char MD5, returned as-is

	// SSE-C object: the stored ETag is longer than 32 chars and its public form
	// is the trailing 32 chars, derived by getDecryptedETag without any key.
	ssecPublic := "11112222333344445555666677778888"
	ssecStored := "abcdef0123456789abcdef0123456789" + ssecPublic // 64 chars
	ssecMeta := map[string]string{crypto.MetaSealedKeySSEC: "test-sealed-key"}

	testCases := []struct {
		name    string
		ifMatch string
		oi      ObjectInfo
		want    bool // true => precondition failed (delete refused, 412)
	}{
		{"live matching etag", plainETag, ObjectInfo{ETag: plainETag}, false},
		{"live matching quoted etag", `"` + plainETag + `"`, ObjectInfo{ETag: plainETag}, false},
		{"live non-matching etag", "0badbeef0badbeef0badbeef0badbeef", ObjectInfo{ETag: plainETag}, true},
		{"live wildcard", "*", ObjectInfo{ETag: plainETag}, false},
		{"live wildcard padded", "  *  ", ObjectInfo{ETag: plainETag}, false},
		{"delete marker concrete etag", plainETag, ObjectInfo{DeleteMarker: true}, true},
		{"delete marker wildcard", "*", ObjectInfo{DeleteMarker: true}, true},
		{"ssec matching public etag", ssecPublic, ObjectInfo{ETag: ssecStored, UserDefined: ssecMeta}, false},
		{"ssec wildcard", "*", ObjectInfo{ETag: ssecStored, UserDefined: ssecMeta}, false},
		{"ssec non-matching etag", "99998888777766665555444433332222", ObjectInfo{ETag: ssecStored, UserDefined: ssecMeta}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deleteIfMatchPreconditionFailed(http.Header{}, tc.ifMatch, tc.oi); got != tc.want {
				t.Errorf("deleteIfMatchPreconditionFailed(%q) = %v, want %v", tc.ifMatch, got, tc.want)
			}
		})
	}
}
