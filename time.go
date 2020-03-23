package kinesisvideomanager

import (
	"fmt"
	"time"
)

func ToTimestamp(t time.Time) string {
	unix := fmt.Sprintf("%d", t.Unix())
	if millis := t.Nanosecond() / int(time.Millisecond); millis > 0 {
		return fmt.Sprintf("%s.%03d", unix, millis)
	}
	return unix
}
