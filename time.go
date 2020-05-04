package kinesisvideomanager

import (
	"fmt"
	"math/big"
	"time"
)

var durationSecond = new(big.Rat).SetInt64(int64(time.Second))

func ToTimestamp(t time.Time) string {
	unix := fmt.Sprintf("%d", t.Unix())
	if millis := t.Nanosecond() / int(time.Millisecond); millis > 0 {
		return fmt.Sprintf("%s.%03d", unix, millis)
	}
	return unix
}

func ParseTimestamp(timestamp string) (time.Time, error) {
	seconds, ok := new(big.Rat).SetString(timestamp)
	if !ok {
		return time.Time{}, fmt.Errorf("failed to parse timestamp: %s", timestamp)
	}
	nanoSec := new(big.Rat).Mul(seconds, durationSecond)
	if !nanoSec.IsInt() {
		return time.Time{}, fmt.Errorf("timestamp might not be Unix time: %s", timestamp)
	}
	return time.Unix(0, nanoSec.Num().Int64()), nil
}
