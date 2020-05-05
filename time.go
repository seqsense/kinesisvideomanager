package kinesisvideomanager

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ToTimestamp(t time.Time) string {
	unix := fmt.Sprintf("%d", t.Unix())
	if millis := t.Nanosecond() / int(time.Millisecond); millis > 0 {
		return fmt.Sprintf("%s.%03d", unix, millis)
	}
	return unix
}

func ParseTimestamp(timestamp string) (time.Time, error) {
	secNano := strings.Split(timestamp, ".")
	if len(secNano) != 1 && len(secNano) != 2 {
		return time.Time{}, fmt.Errorf("failed to parse timestamp: %s", timestamp)
	}
	seconds, err := strconv.ParseInt(secNano[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if len(secNano) == 1 {
		return time.Unix(seconds, 0), nil
	}
	nanoSec, err := strconv.ParseInt((secNano[1] + "000000000")[:9], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, nanoSec), nil
}
