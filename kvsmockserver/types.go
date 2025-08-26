// Copyright 2025 SEQSENSE, Inc.
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

package kvsmockserver

type listFragmentsInput struct {
	FragmentSelector *fragmentSelector
	MaxResults       *int64
	NextToken        *string
	StreamARN        *string
	StreamName       *string
}

type fragmentSelector struct {
	FragmentSelectorType string
	TimestampRange       *timestampRange
}

type timestampRange struct {
	EndTimestamp   *float64 `json:",omitempty"`
	StartTimestamp *float64 `json:",omitempty"`
}

type listFragmentsOutput struct {
	Fragments []fragment
	NextToken *string
}

type fragment struct {
	FragmentLengthInMilliseconds int64
	FragmentNumber               *string
	FragmentSizeInBytes          int64
	ProducerTimestamp            *float64
	ServerTimestamp              *float64
}
