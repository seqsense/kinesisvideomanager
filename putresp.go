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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// ErrorID represents ErrorId enum of PutMedia API.
// See https://docs.aws.amazon.com/kinesisvideostreams/latest/dg/API_dataplane_PutMedia.html for details.
type ErrorID int

const (
	STREAM_READ_ERROR                      ErrorID = 4000
	MAX_FRAGMENT_SIZE_REACHED              ErrorID = 4001
	MAX_FRAGMENT_DURATION_REACHED          ErrorID = 4002
	MAX_CONNECTION_DURATION_REACHED        ErrorID = 4003
	FRAGMENT_TIMECODE_LESSER_THAN_PREVIOUS ErrorID = 4004
	MORE_THAN_ALLOWED_TRACKS_FOUND         ErrorID = 4005
	INVALID_MKV_DATA                       ErrorID = 4006
	INVALID_PRODUCER_TIMESTAMP             ErrorID = 4007
	STREAM_NOT_ACTIVE                      ErrorID = 4008
	FRAGMENT_METADATA_LIMIT_REACHED        ErrorID = 4009
	TRACK_NUMBER_MISMATCH                  ErrorID = 4010
	FRAMES_MISSING_FOR_TRACK               ErrorID = 4011
	KMS_KEY_ACCESS_DENIED                  ErrorID = 4500
	KMS_KEY_DISABLED                       ErrorID = 4501
	KMS_KEY_VALIDATION_ERROR               ErrorID = 4502
	KMS_KEY_UNAVAILABLE                    ErrorID = 4503
	KMS_KEY_INVALID_USAGE                  ErrorID = 4504
	KMS_KEY_INVALID_STATE                  ErrorID = 4505
	KMS_KEY_NOT_FOUND                      ErrorID = 4506
	INTERNAL_ERROR                         ErrorID = 5000
	ARCHIVAL_ERROR                         ErrorID = 5001
)

// FragmentEventType represents AckEventType enum of PutMedia API.
// See https://docs.aws.amazon.com/kinesisvideostreams/latest/dg/API_dataplane_PutMedia.html for details.
type FragmentEventType string

const (
	FRAGMENT_EVENT_BUFFERING FragmentEventType = "BUFFERING"
	FRAGMENT_EVENT_RECEIVED  FragmentEventType = "RECEIVED"
	FRAGMENT_EVENT_PERSISTED FragmentEventType = "PERSISTED"
	FRAGMENT_EVENT_ERROR     FragmentEventType = "ERROR"
	FRAGMENT_EVENT_IDLE      FragmentEventType = "IDLE"
)

// FragmentEvent represents Acknowledgement object of PutMedia API.
// See https://docs.aws.amazon.com/kinesisvideostreams/latest/dg/API_dataplane_PutMedia.html for details.
type FragmentEvent struct {
	EventType        FragmentEventType
	FragmentTimecode uint64
	FragmentNumber   string // 158-bit number, handle as string
	ErrorId          ErrorID
	ErrorCode        string

	fragmentHead []byte
}

func (e *FragmentEvent) IsError() bool {
	return e.EventType == FRAGMENT_EVENT_ERROR
}

func (e *FragmentEvent) AsError() error {
	if e.EventType != "ERROR" {
		panic("non-error FragmentEvent is used as error")
	}
	return &FragmentEventError{FragmentEvent: *e}
}

func (e *FragmentEvent) Dump() []byte {
	return e.fragmentHead
}

type FragmentEventError struct {
	FragmentEvent
}

func (e FragmentEventError) Error() string {
	var dump string
	if len(e.fragmentHead) > 0 {
		dump = `, Data: "` + base64.RawStdEncoding.EncodeToString(e.fragmentHead) + `"`
	}
	return fmt.Sprintf(`fragment event error: { Timecode: %d, FragmentNumber: %s, ErrorId: %d, ErrorCode: "%s"%s }`,
		e.FragmentTimecode, e.FragmentNumber, e.ErrorId, e.ErrorCode, dump,
	)
}

func parseFragmentEvent(r io.Reader, ch chan *FragmentEvent) error {
	defer func() {
		close(ch)
	}()
	dec := json.NewDecoder(r)
	for {
		var fe FragmentEvent
		if err := dec.Decode(&fe); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		ch <- &fe
	}
	return nil
}
