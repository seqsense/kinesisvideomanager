package kinesisvideomanager

import (
	"errors"
	"fmt"
	"strings"
)

type multiErrors []error

func (m multiErrors) Error() string {
	var errs []string
	for _, err := range m {
		errs = append(errs, err.Error())
	}
	return fmt.Sprintf(strings.Join(errs, "; "))
}

func (m multiErrors) Is(err error) bool {
	for _, e := range m {
		if errors.Is(e, err) {
			return true
		}
	}
	return false
}
