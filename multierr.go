// Copyright 2021 SEQSENSE, Inc.
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
	"errors"
)

type multiError []error

func (me multiError) Error() string {
	if len(me) == 1 {
		return me[0].Error()
	}
	str := "multiple errors:"
	for _, e := range me {
		str += " '" + e.Error() + "'"
	}
	return str
}

func (me multiError) Is(err error) bool {
	for _, e := range me {
		if errors.Is(e, err) {
			return true
		}
	}
	return false
}

func (me multiError) As(target interface{}) bool {
	for _, e := range me {
		if errors.As(e, target) {
			return true
		}
	}
	return false
}

func (me *multiError) Add(err error) {
	if err == nil {
		return
	}
	*me = append(*me, err)
}
