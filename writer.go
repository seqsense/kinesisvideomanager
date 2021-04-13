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
	"io"
	"sync/atomic"
)

type ignoreErrWriter struct {
	io.Writer
	err atomic.Value // error
}

func (w *ignoreErrWriter) Write(b []byte) (int, error) {
	if err := w.err.Load(); err != nil {
		return len(b), nil
	}
	if _, err := w.Writer.Write(b); err != nil {
		w.err.Store(err)
	}
	return len(b), nil
}

func (w *ignoreErrWriter) Err() error {
	err, ok := w.err.Load().(error)
	if !ok {
		return nil
	}
	return err
}
