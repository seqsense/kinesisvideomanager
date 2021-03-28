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
	"encoding/json"
	"fmt"
	"io"
)

type FragmentEvent struct {
	EventType        string
	FragmentTimecode uint64
	FragmentNumber   string // 158-bit number, handle as string
	ErrorId          int
	ErrorCode        string
}

func (e *FragmentEvent) IsError() bool {
	return e.EventType == "ERROR"
}

func (e *FragmentEvent) Error() string {
	if e.EventType != "ERROR" {
		panic("non-error FragmentEvent is used as error")
	}
	return fmt.Sprintf("fragment event error: { Timecode: %d, FragmentNumber: %s, ErrorId: %d, ErrorCode: %s }",
		e.FragmentTimecode, e.FragmentNumber, e.ErrorId, e.ErrorCode,
	)
}

func parseFragmentEvent(r io.Reader) ([]FragmentEvent, error) {
	dec := json.NewDecoder(r)
	var ret []FragmentEvent
	for {
		var fe FragmentEvent
		if err := dec.Decode(&fe); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		ret = append(ret, fe)
	}
	return ret, nil
}
