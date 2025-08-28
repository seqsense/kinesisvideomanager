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

package mediafragment

import (
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	kvam "github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia"
	kvam_types "github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia/types"
)

func TestListFragmentsOutput(t *testing.T) {
	testData := func() *ListFragmentsOutput {
		return &ListFragmentsOutput{
			&kvam.ListFragmentsOutput{
				Fragments: []kvam_types.Fragment{
					{FragmentNumber: aws.String("1234568000"), ProducerTimestamp: aws.Time(time.Unix(100005, 0))},
					{FragmentNumber: aws.String("1234567000"), ProducerTimestamp: aws.Time(time.Unix(100000, 0))},
					{FragmentNumber: aws.String("1234569000"), ProducerTimestamp: aws.Time(time.Unix(100015, 0))},
					{FragmentNumber: aws.String("1234570000"), ProducerTimestamp: aws.Time(time.Unix(100010, 0))},
				},
			},
		}
	}
	t.Run("SortByFragmentNumber", func(t *testing.T) {
		expected := &ListFragmentsOutput{
			&kvam.ListFragmentsOutput{
				Fragments: []kvam_types.Fragment{
					{FragmentNumber: aws.String("1234567000"), ProducerTimestamp: aws.Time(time.Unix(100000, 0))},
					{FragmentNumber: aws.String("1234568000"), ProducerTimestamp: aws.Time(time.Unix(100005, 0))},
					{FragmentNumber: aws.String("1234569000"), ProducerTimestamp: aws.Time(time.Unix(100015, 0))},
					{FragmentNumber: aws.String("1234570000"), ProducerTimestamp: aws.Time(time.Unix(100010, 0))},
				},
			},
		}

		data := testData()
		data.SortByFragmentNumber()
		if !reflect.DeepEqual(expected, data) {
			t.Errorf("Expected sort result:\n%v\ngot:\n%v", expected, data)
		}

		t.Run("Sort_alias", func(t *testing.T) {
			data := testData()
			data.Sort()
			if !reflect.DeepEqual(expected, data) {
				t.Errorf("Expected sort result:\n%v\ngot:\n%v", expected, data)
			}
		})
	})
	t.Run("SortByProducerTimestamp", func(t *testing.T) {
		expected := &ListFragmentsOutput{
			&kvam.ListFragmentsOutput{
				Fragments: []kvam_types.Fragment{
					{FragmentNumber: aws.String("1234567000"), ProducerTimestamp: aws.Time(time.Unix(100000, 0))},
					{FragmentNumber: aws.String("1234568000"), ProducerTimestamp: aws.Time(time.Unix(100005, 0))},
					{FragmentNumber: aws.String("1234570000"), ProducerTimestamp: aws.Time(time.Unix(100010, 0))},
					{FragmentNumber: aws.String("1234569000"), ProducerTimestamp: aws.Time(time.Unix(100015, 0))},
				},
			},
		}

		data := testData()
		data.SortByProducerTimestamp()
		if !reflect.DeepEqual(expected, data) {
			t.Errorf("Expected sort result:\n%v\ngot:\n%v", expected, data)
		}
	})
	t.Run("Uniq", func(t *testing.T) {
		data := &ListFragmentsOutput{
			&kvam.ListFragmentsOutput{
				Fragments: []kvam_types.Fragment{
					{FragmentNumber: aws.String("1234566000"), ProducerTimestamp: aws.Time(time.Unix(100000, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234566100"), ProducerTimestamp: aws.Time(time.Unix(100000, 0)), FragmentLengthInMilliseconds: 30},
					{FragmentNumber: aws.String("1234567000"), ProducerTimestamp: aws.Time(time.Unix(100005, 0)), FragmentLengthInMilliseconds: 90},
					{FragmentNumber: aws.String("1234567100"), ProducerTimestamp: aws.Time(time.Unix(100005, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234567200"), ProducerTimestamp: aws.Time(time.Unix(100005, 0)), FragmentLengthInMilliseconds: 80},
					{FragmentNumber: aws.String("1234568000"), ProducerTimestamp: aws.Time(time.Unix(100010, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234569000"), ProducerTimestamp: aws.Time(time.Unix(100015, 0)), FragmentLengthInMilliseconds: 90},
					{FragmentNumber: aws.String("1234569100"), ProducerTimestamp: aws.Time(time.Unix(100015, 0)), FragmentLengthInMilliseconds: 100},
				},
			},
		}
		expected := &ListFragmentsOutput{
			&kvam.ListFragmentsOutput{
				Fragments: []kvam_types.Fragment{
					{FragmentNumber: aws.String("1234566000"), ProducerTimestamp: aws.Time(time.Unix(100000, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234567100"), ProducerTimestamp: aws.Time(time.Unix(100005, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234568000"), ProducerTimestamp: aws.Time(time.Unix(100010, 0)), FragmentLengthInMilliseconds: 100},
					{FragmentNumber: aws.String("1234569100"), ProducerTimestamp: aws.Time(time.Unix(100015, 0)), FragmentLengthInMilliseconds: 100},
				},
			},
		}

		data.Uniq()
		if !reflect.DeepEqual(expected, data) {
			t.Errorf("Expected sort result:\n%v\ngot:\n%v", expected, data)
		}
	})
}
