// Copyright (c) 2026 PGSTY
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

package s3select

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

// decodeEvents parses a recorded event stream with the minio-go client and
// returns the concatenated record payloads and the terminal error, if any.
func decodeEvents(t *testing.T, response []byte) ([]byte, error) {
	t.Helper()
	body := &testResponseBody{Reader: bytes.NewReader(response), closed: make(chan struct{})}
	res, err := minio.NewSelectResults(&http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: int64(len(response))}, "testbucket")
	if err != nil {
		t.Fatal(err)
	}
	records, readErr := io.ReadAll(res)
	<-body.closed
	return records, readErr
}

func newTestRecord(content string) *bytes.Buffer {
	payload := bufPool.Get()
	payload.Reset()
	payload.WriteString(content)
	return payload
}

// eventOrder returns the byte offsets of the given markers in the response,
// or -1 for a marker that is absent.
func eventOrder(response []byte, markers ...string) []int {
	offsets := make([]int, len(markers))
	for i, marker := range markers {
		offsets[i] = bytes.Index(response, []byte(marker))
	}
	return offsets
}

func ascending(offsets []int) bool {
	for i, offset := range offsets {
		if offset < 0 || (i > 0 && offset <= offsets[i-1]) {
			return false
		}
	}
	return true
}

// TestMessageWriterFlushesQueuedRecordBeforeError sends a record and an error
// back to back, so the writer goroutine sees both channels ready and picks
// one at random. The record must appear in the response before the error
// message every time.
func TestMessageWriterFlushesQueuedRecordBeforeError(t *testing.T) {
	for i := range 200 {
		w := &testResponseWriter{}
		writer := newMessageWriter(w, nil)
		payload := bufPool.Get()
		payload.Reset()
		payload.WriteString(`{"id":1}` + "\n")
		if err := writer.SendRecord(payload); err != nil {
			t.Fatalf("run %d: SendRecord: %v", i, err)
		}
		if err := writer.FinishWithError("OverMaxRecordSize", "too large"); err != nil {
			t.Fatalf("run %d: FinishWithError: %v", i, err)
		}
		record := bytes.Index(w.response, []byte(`{"id":1}`))
		errMsg := bytes.Index(w.response, []byte("OverMaxRecordSize"))
		if record < 0 || errMsg < 0 || record > errMsg {
			t.Fatalf("run %d: record at %d, error at %d: queued record was dropped or reordered", i, record, errMsg)
		}
	}
}

// TestMessageWriterFlushesBufferedAndQueuedRecordsBeforeError: one record has
// already been staged into the buffer and a second one is still queued when
// the error arrives. Both must precede the error, in order.
func TestMessageWriterFlushesBufferedAndQueuedRecordsBeforeError(t *testing.T) {
	for i := range 100 {
		w := &testResponseWriter{}
		writer := newMessageWriter(w, nil)
		if err := writer.SendRecord(newTestRecord(`{"id":1}` + "\n")); err != nil {
			t.Fatal(err)
		}
		// Give the writer a chance to stage the first record; whether it
		// did or not, the outcome must be the same.
		if i%2 == 0 {
			for range 100 {
				if len(writer.payloadCh) == 0 {
					break
				}
			}
		}
		if err := writer.SendRecord(newTestRecord(`{"id":2}` + "\n")); err != nil {
			t.Fatal(err)
		}
		if err := writer.FinishWithError("OverMaxRecordSize", "too large"); err != nil {
			t.Fatal(err)
		}
		if got := eventOrder(w.response, `{"id":1}`, `{"id":2}`, "OverMaxRecordSize"); !ascending(got) {
			t.Fatalf("run %d: offsets %v: records must precede the error in order", i, got)
		}
	}
}

// TestMessageWriterErrorWithoutRecords: an error with nothing queued writes
// only the error message, no empty Records event.
func TestMessageWriterErrorWithoutRecords(t *testing.T) {
	w := &testResponseWriter{}
	writer := newMessageWriter(w, nil)
	if err := writer.FinishWithError("InternalError", "boom"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(w.response, []byte("InternalError")) || bytes.Contains(w.response, []byte("Records")) {
		t.Fatalf("unexpected response: %q", w.response)
	}
}

// TestMessageWriterStagesRecordsLargerThanTheBuffer: a record larger than the
// staging buffer is split across several Records events and nothing is lost
// or reordered, whether the stream ends with success or with an error.
func TestMessageWriterStagesRecordsLargerThanTheBuffer(t *testing.T) {
	big := strings.Repeat("x", bufLength+bufLength/2)
	want := "head\n" + big + "\n" + "tail\n"
	for _, withError := range []bool{false, true} {
		w := &testResponseWriter{}
		writer := newMessageWriter(w, nil)
		for _, rec := range []string{"head\n", big + "\n", "tail\n"} {
			if err := writer.SendRecord(newTestRecord(rec)); err != nil {
				t.Fatal(err)
			}
		}
		if withError {
			if err := writer.FinishWithError("OverMaxRecordSize", "too large"); err != nil {
				t.Fatal(err)
			}
		} else if err := writer.Finish(10, 10); err != nil {
			t.Fatal(err)
		}
		records, err := decodeEvents(t, w.response)
		if string(records) != want {
			t.Fatalf("withError=%v: got %d record bytes, want %d; head=%q", withError, len(records), len(want), string(records[:min(len(records), 8)]))
		}
		if withError && (err == nil || !strings.Contains(err.Error(), "OverMaxRecordSize")) {
			t.Fatalf("expected OverMaxRecordSize after the records, got %v", err)
		}
		if !withError && err != nil {
			t.Fatalf("unexpected error on the success path: %v", err)
		}
	}
}

// TestMessageWriterSuccessOrder: the success path is unchanged: every record,
// then Stats, then End, and the client sees no error.
func TestMessageWriterSuccessOrder(t *testing.T) {
	w := &testResponseWriter{}
	writer := newMessageWriter(w, nil)
	for _, rec := range []string{`{"id":1}`, `{"id":2}`} {
		if err := writer.SendRecord(newTestRecord(rec + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Finish(20, 20); err != nil {
		t.Fatal(err)
	}
	if got := eventOrder(w.response, `{"id":1}`, `{"id":2}`, "Stats", "End"); !ascending(got) {
		t.Fatalf("offsets %v", got)
	}
	records, err := decodeEvents(t, w.response)
	if err != nil || string(records) != "{\"id\":1}\n{\"id\":2}\n" {
		t.Fatalf("records %q err %v", records, err)
	}
}
