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
	"testing"
)

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
