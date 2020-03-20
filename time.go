package kinesisvideomanager

import (
	"fmt"
	"strconv"
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
	unix, err := strconv.ParseFloat(timestamp, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, int64(unix*float64(time.Second))), nil
}
