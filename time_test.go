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
