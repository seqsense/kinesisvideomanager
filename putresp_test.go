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
	"testing"
)

func TestFragmentEvent(t *testing.T) {
	input := `{"EventType":"ERROR","FragmentTimecode":12345,"FragmentNumber":"91343852333754009371412493862204112772176002064","ErrorId":5000,"ErrorCode":"DUMMY_ERROR"}`
	var fe FragmentEvent
	if err := json.Unmarshal([]byte(input), &fe); err != nil {
		t.Fatal(err)
	}

	expected := `fragment event error: { Timecode: 12345, FragmentNumber: 91343852333754009371412493862204112772176002064, ErrorId: 5000, ErrorCode: "DUMMY_ERROR" }`
	if s := fe.AsError().Error(); s != expected {
		t.Errorf("Expected error string:\n%s\ngot:\n%s", expected, s)
	}
}
