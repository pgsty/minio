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

package hash

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	xhttp "github.com/minio/minio/internal/http"
)

func TestGetContentChecksumRejectsUnsupportedHeaders(t *testing.T) {
	unsupported := []string{
		"x-amz-checksum-md5",
		"x-amz-checksum-sha512",
		"x-amz-checksum-xxhash64",
		"x-amz-checksum-xxhash3",
		"x-amz-checksum-xxhash128",
		"x-amz-checksum-future",
	}
	for _, header := range unsupported {
		t.Run("header/"+header, func(t *testing.T) {
			h := http.Header{header: {"AA=="}}
			if _, err := GetContentChecksum(h); !errors.Is(err, ErrInvalidChecksum) {
				t.Fatalf("GetContentChecksum(%s) error = %v, want ErrInvalidChecksum", header, err)
			}
		})
		t.Run("trailer/"+header, func(t *testing.T) {
			h := http.Header{xhttp.AmzTrailer: {header}}
			if _, err := GetContentChecksum(h); !errors.Is(err, ErrInvalidChecksum) {
				t.Fatalf("GetContentChecksum(trailer %s) error = %v, want ErrInvalidChecksum", header, err)
			}
		})
	}

	for header, value := range map[string]string{
		xhttp.AmzChecksumAlgo:          "CRC32",
		xhttp.AmzChecksumType:          xhttp.AmzChecksumTypeComposite,
		xhttp.AmzChecksumMode:          "ENABLED",
		"x-amz-sdk-checksum-algorithm": "SHA512",
	} {
		t.Run("control/"+header, func(t *testing.T) {
			h := http.Header{header: {value}}
			if _, err := GetContentChecksum(h); errors.Is(err, ErrInvalidChecksum) {
				t.Fatalf("control header %s was rejected", header)
			}
		})
	}
}

// TestChecksumAddToHeader tests that adding and retrieving a checksum on a header works
func TestChecksumAddToHeader(t *testing.T) {
	if got := NewChecksumType("CRC64NVME", xhttp.AmzChecksumTypeComposite); !got.Is(ChecksumInvalid) {
		t.Fatalf("CRC64NVME/COMPOSITE = %s, want invalid", got.StringFull())
	}
	tests := []struct {
		name     string
		checksum ChecksumType
		fullobj  bool
		wantErr  bool
	}{
		{"CRC32-composite", ChecksumCRC32, false, false},
		{"CRC32-full-object", ChecksumCRC32, true, false},
		{"CRC32C-composite", ChecksumCRC32C, false, false},
		{"CRC32C-full-object", ChecksumCRC32C, true, false},
		{"CRC64NVME-full-object", ChecksumCRC64NVME, false, false}, // CRC64NVME is always full object
		{"ChecksumSHA1-composite", ChecksumSHA1, false, false},
		{"ChecksumSHA256-composite", ChecksumSHA256, false, false},
		{"ChecksumSHA1-full-object", ChecksumSHA1, true, true},     // SHA1 does not support full object
		{"ChecksumSHA256-full-object", ChecksumSHA256, true, true}, // SHA256 does not support full object
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip invalid cases where SHA1 or SHA256 is used with full object
			if (tt.checksum.Is(ChecksumSHA1) || tt.checksum.Is(ChecksumSHA256)) && tt.fullobj {
				// Validate that NewChecksumType correctly marks these as invalid
				alg := tt.checksum.String()
				typ := NewChecksumType(alg, xhttp.AmzChecksumTypeFullObject)
				if !typ.Is(ChecksumInvalid) {
					t.Fatalf("Expected ChecksumInvalid for %s with full object, got %s", tt.name, typ.StringFull())
				}
				return
			}
			myData := []byte("this-is-a-checksum-data-test")
			chksm := NewChecksumFromData(tt.checksum, myData)
			if chksm == nil {
				t.Fatalf("NewChecksumFromData failed for %s", tt.name)
			}
			if tt.fullobj {
				chksm.Type |= ChecksumFullObject
			}

			// CRC64NVME is always full object
			if chksm.Type.Base().Is(ChecksumCRC64NVME) {
				chksm.Type |= ChecksumFullObject
			}

			// Prepare the checksum map with appropriate headers
			m := chksm.AsMap()
			m[xhttp.AmzChecksumAlgo] = chksm.Type.String() // Set the algorithm explicitly
			if chksm.Type.FullObjectRequested() {
				m[xhttp.AmzChecksumType] = xhttp.AmzChecksumTypeFullObject
			} else {
				m[xhttp.AmzChecksumType] = xhttp.AmzChecksumTypeComposite
			}

			w := httptest.NewRecorder()
			AddChecksumHeader(w, m)
			gotChksm, err := GetContentChecksum(w.Result().Header)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error for %s, got none", tt.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetContentChecksum failed for %s: %v", tt.name, err)
			}

			if gotChksm == nil {
				t.Fatalf("Got nil checksum for %s", tt.name)
			}
			// Compare the full checksum structs
			if !chksm.Equal(gotChksm) {
				t.Errorf("Checksum mismatch for %s: expected %+v, got %+v", tt.name, chksm, gotChksm)
			}
			// Verify the checksum type
			expectedType := chksm.Type
			if gotChksm.Type != expectedType {
				t.Errorf("Type mismatch for %s: expected %s, got %s", tt.name, expectedType.StringFull(), gotChksm.Type.StringFull())
			}
		})
	}
}

func TestCRC64NVMECompositeTrailerIsInvalid(t *testing.T) {
	h := http.Header{}
	h.Set(xhttp.AmzTrailer, ChecksumCRC64NVME.Key())
	h.Set(xhttp.AmzChecksumType, xhttp.AmzChecksumTypeComposite)
	_, err := GetContentChecksum(h)
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("CRC64NVME/COMPOSITE trailer error = %v, want ErrInvalidChecksum", err)
	}
}

// TestChecksumSerializeDeserialize checks AppendTo can be reversed by ChecksumFromBytes
func TestChecksumSerializeDeserialize(t *testing.T) {
	myData := []byte("this-is-a-checksum-data-test")
	chksm := NewChecksumFromData(ChecksumCRC32, myData)
	if chksm == nil {
		t.Fatal("NewChecksumFromData returned nil")
	}
	// Serialize the checksum to bytes
	b := chksm.AppendTo(nil, nil)
	if b == nil {
		t.Fatal("AppendTo returned nil")
	}

	// Deserialize the checksum from bytes
	chksmOut := ChecksumFromBytes(b)
	if chksmOut == nil {
		t.Fatal("ChecksumFromBytes returned nil")
	}

	// Assert new checksum matches the content
	matchError := chksmOut.Matches(myData, 0)
	if matchError != nil {
		t.Fatalf("Checksum mismatch on chksmOut: %v", matchError)
	}

	// Assert they are exactly equal
	if !chksmOut.Equal(chksm) {
		t.Fatalf("Checksum mismatch: expected %+v, got %+v", chksm, chksmOut)
	}
}

// TestChecksumSerializeDeserializeMultiPart checks AppendTo can be reversed by ChecksumFromBytes
// for multipart checksum
func TestChecksumSerializeDeserializeMultiPart(t *testing.T) {
	// Create dummy data that we'll split into 3 parts
	dummyData := []byte("The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen brown eggs. " +
		"Have another go it will all make sense in the end!")

	// Split data into 3 parts
	partSize := len(dummyData) / 3
	part1Data := dummyData[0:partSize]
	part2Data := dummyData[partSize : 2*partSize]
	part3Data := dummyData[2*partSize:]

	// Calculate CRC32C checksum for each part using NewChecksumFromData
	checksumType := ChecksumCRC32C

	part1Checksum := NewChecksumFromData(checksumType, part1Data)
	part2Checksum := NewChecksumFromData(checksumType, part2Data)
	part3Checksum := NewChecksumFromData(checksumType, part3Data)

	// Combine the raw checksums (this is what happens in CompleteMultipartUpload)
	var checksumCombined []byte
	checksumCombined = append(checksumCombined, part1Checksum.Raw...)
	checksumCombined = append(checksumCombined, part2Checksum.Raw...)
	checksumCombined = append(checksumCombined, part3Checksum.Raw...)

	// Create the final checksum (checksum of the combined checksums)
	// Add BOTH the multipart flag AND the includes-multipart flag
	finalChecksumType := checksumType | ChecksumMultipart | ChecksumIncludesMultipart
	finalChecksum := NewChecksumFromData(finalChecksumType, checksumCombined)

	// Set WantParts to indicate 3 parts
	finalChecksum.WantParts = 3

	// Test AppendTo serialization
	var serialized []byte
	serialized = finalChecksum.AppendTo(serialized, checksumCombined)

	// Use ChecksumFromBytes to deserialize the final checksum
	chksmOut := ChecksumFromBytes(serialized)
	if chksmOut == nil {
		t.Fatal("ChecksumFromBytes returned nil")
	}

	// Assert they are exactly equal
	if !chksmOut.Equal(finalChecksum) {
		t.Fatalf("Checksum mismatch: expected %+v, got %+v", finalChecksum, chksmOut)
	}

	// Serialize what we got from ChecksumFromBytes
	serializedOut := chksmOut.AppendTo(nil, checksumCombined)

	// Read part checksums from serializedOut
	readParts := ReadPartCheckSums(serializedOut)
	expectedChecksums := []string{
		part1Checksum.Encoded,
		part2Checksum.Encoded,
		part3Checksum.Encoded,
	}
	for i, expected := range expectedChecksums {
		if got := readParts[i][ChecksumCRC32C.String()]; got != expected {
			t.Fatalf("want part%dChecksum.Encoded %s, got %s", i+1, expected, got)
		}
	}
}

// TestGetContentChecksumTrailerWithHeaderValue covers the case where a checksum
// is advertised via x-amz-trailer while its value is delivered as a request
// header (no trailer is actually sent). The AWS Java SDK v2 does this on chunked
// (aws-chunked) uploads that use STREAMING-AWS4-HMAC-SHA256-PAYLOAD (non-trailer)
// but still list the checksum in x-amz-trailer. See issue #107. The header value
// must be honored as a non-trailing checksum instead of being treated as an empty
// trailing checksum.
func TestGetContentChecksumTrailerWithHeaderValue(t *testing.T) {
	const crc = "Hkksgg==" // CRC32 of "Hello CRC32!"

	// Trailer advertised AND value present in header -> non-trailing, value honored.
	h := http.Header{}
	h.Set(xhttp.AmzTrailer, xhttp.AmzChecksumCRC32)
	h.Set(xhttp.AmzChecksumCRC32, crc)
	cs, err := GetContentChecksum(h)
	if err != nil {
		t.Fatalf("GetContentChecksum error = %v, want nil", err)
	}
	if cs == nil {
		t.Fatal("GetContentChecksum returned nil checksum")
	}
	if cs.Type.Trailing() {
		t.Errorf("checksum reported as trailing; want non-trailing since value is in the header")
	}
	if !cs.Type.Is(ChecksumCRC32) {
		t.Errorf("checksum type = %s, want CRC32", cs.Type.StringFull())
	}
	if cs.Encoded != crc {
		t.Errorf("checksum value = %q, want %q", cs.Encoded, crc)
	}

	// Trailer advertised WITHOUT a header value -> stays trailing (unchanged).
	h2 := http.Header{}
	h2.Set(xhttp.AmzTrailer, xhttp.AmzChecksumCRC32)
	cs2, err := GetContentChecksum(h2)
	if err != nil {
		t.Fatalf("GetContentChecksum (no header value) error = %v, want nil", err)
	}
	if cs2 == nil || !cs2.Type.Trailing() {
		t.Errorf("checksum = %v, want a trailing CRC32 checksum", cs2)
	}
}

// TestGetContentChecksumTrailerMalformedHeaderValue guards against turning a
// malformed client-supplied checksum into a no-op. When a checksum is advertised
// via x-amz-trailer and its header value is present but does not parse, the
// request must be rejected (ErrInvalidChecksum) rather than silently dropped.
// The mismatched x-amz-checksum-algorithm selector makes the regression visible:
// without the guard, execution falls through to getContentChecksum which would
// return (nil, nil) and install no validator at all.
func TestGetContentChecksumTrailerMalformedHeaderValue(t *testing.T) {
	h := http.Header{}
	h.Set(xhttp.AmzTrailer, xhttp.AmzChecksumCRC32)
	h.Set(xhttp.AmzChecksumCRC32, "AQID") // decodes to 3 bytes -> invalid CRC32
	h.Set(xhttp.AmzChecksumAlgo, "SHA256")
	cs, err := GetContentChecksum(h)
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("GetContentChecksum error = %v (checksum %v), want ErrInvalidChecksum", err, cs)
	}

	// Same, without the misleading algorithm selector: still an error.
	h2 := http.Header{}
	h2.Set(xhttp.AmzTrailer, xhttp.AmzChecksumCRC32)
	h2.Set(xhttp.AmzChecksumCRC32, "AQID")
	if _, err := GetContentChecksum(h2); !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("GetContentChecksum (no algo selector) error = %v, want ErrInvalidChecksum", err)
	}
}
