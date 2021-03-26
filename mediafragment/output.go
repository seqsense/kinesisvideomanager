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

package mediafragment

import (
	"sort"

	kvam "github.com/aws/aws-sdk-go/service/kinesisvideoarchivedmedia"
)

type ListFragmentsOutput struct {
	*kvam.ListFragmentsOutput
}

type FragmentIDs []*string

func NewFragmentIDs(ids ...string) FragmentIDs {
	var ret FragmentIDs
	for _, id := range ids {
		ret = append(ret, &id)
	}
	return ret
}

// Sort is alias of SortByFragmentNumber.
//
// Deprecated: use SortByFragmentNumber or SortByProducerTimestamp
func (l *ListFragmentsOutput) Sort() {
	l.SortByFragmentNumber()
}

func (l *ListFragmentsOutput) SortByFragmentNumber() {
	sort.Sort(SortByFragmentNumber{l})
}

func (l *ListFragmentsOutput) SortByProducerTimestamp() {
	sort.Sort(SortByProducerTimestamp{l})
}

func (l ListFragmentsOutput) Len() int {
	return len(l.Fragments)
}

func (l *ListFragmentsOutput) Swap(i, j int) {
	l.Fragments[i], l.Fragments[j] = l.Fragments[j], l.Fragments[i]
}

type SortByFragmentNumber struct {
	*ListFragmentsOutput
}

func (l SortByFragmentNumber) Less(i, j int) bool {
	return *l.Fragments[i].FragmentNumber < *l.Fragments[j].FragmentNumber
}

type SortByProducerTimestamp struct {
	*ListFragmentsOutput
}

func (l SortByProducerTimestamp) Less(i, j int) bool {
	return (*l.Fragments[i].ProducerTimestamp).Before(*l.Fragments[j].ProducerTimestamp)
}

func (l *ListFragmentsOutput) FragmentIDs() FragmentIDs {
	var ret FragmentIDs
	for _, f := range l.Fragments {
		ret = append(ret, f.FragmentNumber)
	}
	return ret
}
