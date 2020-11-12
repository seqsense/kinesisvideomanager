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
	"testing"
	"time"
)

func Test_ToTimestamp(t *testing.T) {
	testCases := map[string]struct {
		input    time.Time
		expected string
	}{
		"MillisIsZero": {
			time.Unix(1, int64(time.Millisecond-1)),
			"1",
		},
		"MillisIsOneDigit": {
			time.Unix(0, int64(time.Millisecond)),
			"0.001",
		},
		"MillisIsTwoDigits": {
			time.Unix(0, 12*int64(time.Millisecond)),
			"0.012",
		},
		"MillisIsThreeDigits": {
			time.Unix(0, 123*int64(time.Millisecond)),
			"0.123",
		},
	}
	for n, c := range testCases {
		t.Run(n, func(t *testing.T) {
			ts := ToTimestamp(c.input)
			if ts != c.expected {
				t.Errorf("Expected timestamp: '%v', got: '%v'", c.expected, ts)
			}
		})
	}
}

func Test_ParseTimestamp(t *testing.T) {
	testCases := map[string]struct {
		input    string
		expected time.Time
	}{
		"MillisIsZero": {
			"1",
			time.Unix(1, 0),
		},
		"MillisIsOneDigit": {
			"0.001",
			time.Unix(0, int64(time.Millisecond)),
		},
		"MillisIsTwoDigits": {
			"0.012",
			time.Unix(0, 12*int64(time.Millisecond)),
		},
		"MillisIsThreeDigits": {
			"0.123",
			time.Unix(0, 123*int64(time.Millisecond)),
		},
		"ConsiderFloatingPointError": {
			"1000000000.607",
			time.Unix(1000000000, 607*int64(time.Millisecond)),
		},
	}
	for n, c := range testCases {
		t.Run(n, func(t *testing.T) {
			ts, err := ParseTimestamp(c.input)
			if err != nil {
				t.Errorf("Failed to parseTimestamp: %v", err)
				t.Fatal(err)
			}
			if ts != c.expected {
				t.Errorf("Expected timestamp: '%v', got: '%v'", c.expected, ts)
			}
		})
	}
}
