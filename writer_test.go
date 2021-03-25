// Copyright 2020 SEQSENSE, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kinesisvideomanager

import (
	"errors"
	"testing"
)

type dummyWriter struct {
	err error
}

func (w *dummyWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return len(b), nil
}

func TestIgnoreErrWriter(t *testing.T) {
	t.Run("Nil", func(t *testing.T) {
		w := &ignoreErrWriter{Writer: &dummyWriter{}}
		n, err := w.Write(make([]byte, 10))
		if n != 10 {
			t.Error("Write length differs")
		}
		if err != nil {
			t.Error("ignoreErrWriter.Write must not return error")
		}
		if err := w.Err(); err != nil {
			t.Errorf("Base writer didn't return error, but ignoreErrWriter stores error: '%v'", err)
		}
	})
	t.Run("Error", func(t *testing.T) {
		dummyErr := errors.New("test")
		w := &ignoreErrWriter{Writer: &dummyWriter{err: dummyErr}}
		n, err := w.Write(make([]byte, 10))
		if n != 0 {
			t.Error("Write length differs")
		}
		if err != nil {
			t.Error("ignoreErrWriter.Write must not return error")
		}
		if err := w.Err(); err != dummyErr {
			t.Errorf("Expected to store '%v', but got '%v'", dummyErr, err)
		}
	})
}
