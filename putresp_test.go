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
	"strings"
	"testing"
)

func gatherResponses() (chan *FragmentEvent, func() []FragmentEvent) {
	chResp := make(chan *FragmentEvent, 1000)
	return chResp, func() []FragmentEvent {
		var fes []FragmentEvent
		for fe := range chResp {
			fes = append(fes, *fe)
		}
		return fes
	}
}

func TestFragmentEvent(t *testing.T) {
	t.Run("ErrorEvent", func(t *testing.T) {
		input := `{"EventType":"ERROR","FragmentTimecode":12345,"FragmentNumber":"91343852333754009371412493862204112772176002064","ErrorId":5000,"ErrorCode":"DUMMY_ERROR"}`
		chResp, gather := gatherResponses()
		if err := parseFragmentEvent(strings.NewReader(input), chResp); err != nil {
			t.Fatal(err)
		}
		fe := gather()

		if n := len(fe); n != 1 {
			t.Fatalf("Expected 1 FragmentEvent, got %d", n)
		}

		expected := `fragment event error: { Timecode: 12345, FragmentNumber: 91343852333754009371412493862204112772176002064, ErrorId: 5000, ErrorCode: "DUMMY_ERROR" }`
		if s := fe[0].AsError().Error(); s != expected {
			t.Errorf("Expected error string:\n%s\ngot:\n%s", expected, s)
		}

		fe[0].fragmentHead = []byte("test")

		expected2 := `fragment event error: { Timecode: 12345, FragmentNumber: 91343852333754009371412493862204112772176002064, ErrorId: 5000, ErrorCode: "DUMMY_ERROR", Data: "dGVzdA" }`
		if s := fe[0].AsError().Error(); s != expected2 {
			t.Errorf("Expected error string:\n%s\ngot:\n%s", expected2, s)
		}
	})
	t.Run("ParseError", func(t *testing.T) {
		chResp, gather := gatherResponses()
		err := parseFragmentEvent(strings.NewReader("{"), chResp)
		if err != io.ErrUnexpectedEOF {
			t.Fatalf("Expected error: '%v', got: '%v'", io.ErrUnexpectedEOF, err)
		}
		fe := gather()
		if n := len(fe); n != 0 {
			t.Fatalf("Expected 0 FragmentEvent, got %d", n)
		}
	})
}
