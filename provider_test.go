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

package kinesisvideomanager_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"

	kvm "github.com/seqsense/kinesisvideomanager"
	kvsm "github.com/seqsense/kinesisvideomanager/kvsmockserver"
)

var testData = [][]byte{{0x01, 0x02}}

func TestProvider(t *testing.T) {
	dropped := make(map[uint64]bool)

	testCases := map[string]struct {
		mockServerOpts []kvsm.KinesisVideoServerOption
		putMediaOpts   []kvm.PutMediaOption
	}{
		"NoError": {},
		"ErrorRetry": {
			mockServerOpts: []kvsm.KinesisVideoServerOption{
				kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
					if !dropped[timecode] {
						dropped[timecode] = true
						w.WriteHeader(500)
						t.Logf("Error injected: timecode=%d", timecode)
						return false
					}
					return true
				}),
			},
			putMediaOpts: []kvm.PutMediaOption{
				kvm.WithRetry(2, 100*time.Millisecond),
			},
		},
	}

	for name, testCase := range testCases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			server := kvsm.NewKinesisVideoServer(testCase.mockServerOpts...)
			defer server.Close()

			pro := newProvider(t, server)

			ch := make(chan *kvm.BlockWithBaseTimecode)
			timecodes := []uint64{
				1000,
				9000,
				10000,
				10001, // switch to the next fragment here
				10002,
			}
			go func() {
				defer close(ch)
				for _, tc := range timecodes {
					ch <- &kvm.BlockWithBaseTimecode{
						Timecode: tc,
						Block:    newBlock(0),
					}
				}
			}()

			chResp := make(chan kvm.FragmentEvent)
			var response []kvm.FragmentEvent
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			go func() {
				defer cancel()
				for {
					select {
					case r, ok := <-chResp:
						if !ok {
							return
						}
						response = append(response, r)
					}
				}
			}()

			startTimestamp := time.Now()
			startTimestampInMillis := uint64(startTimestamp.UnixNano() / int64(time.Millisecond))
			cnt := 0
			var err error
			opts := []kvm.PutMediaOption{
				kvm.WithFragmentTimecodeType(kvm.FragmentTimecodeTypeRelative),
				kvm.WithProducerStartTimestamp(startTimestamp),
				kvm.WithTags(func() []kvm.SimpleTag {
					cnt++
					return []kvm.SimpleTag{
						{TagName: "TEST_TAG", TagString: fmt.Sprintf("%d", cnt)},
					}
				}),
				kvm.OnError(func(e error) {
					err = e
				}),
			}
			opts = append(opts, testCase.putMediaOpts...)
			pro.PutMedia(ch, chResp, opts...)
			if err != nil {
				t.Fatalf("Failed to run PutMedia: %v", err)
			}

			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("PutMedia timed out")
			}

			expected := []kvsm.FragmentTest{
				{
					Cluster: kvsm.ClusterTest{
						Timecode:    startTimestampInMillis + 1000,
						SimpleBlock: []ebml.Block{newBlock(0), newBlock(8000), newBlock(9000)},
					},
					Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "1"}}),
				},
				{
					Cluster: kvsm.ClusterTest{
						Timecode:    startTimestampInMillis + 10001,
						SimpleBlock: []ebml.Block{newBlock(0), newBlock(1)},
					},
					Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "2"}}),
				},
			}

			if n := len(response); n != len(expected) {
				t.Fatalf("Response size expected to be %d but %d", len(expected), n)
			}

			for _, fragment := range expected {
				actual, ok := server.GetFragment(fragment.Cluster.Timecode)
				if !ok {
					t.Errorf("fragment %d not found", fragment.Cluster.Timecode)
					continue
				}
				if !reflect.DeepEqual(fragment.Cluster, actual.Cluster) {
					t.Errorf("Unexpected Cluster\n expected:%+v\n actual%+v", fragment.Cluster, actual.Cluster)
				}
				if !reflect.DeepEqual(fragment.Tags, actual.Tags) {
					t.Errorf("Unexpected Tags\n expected:%+v\n actual%+v", fragment.Tags, actual.Tags)
				}
			}
		})
	}
}

func TestProvider_WithHttpClient(t *testing.T) {
	blockTime := 2 * time.Second
	server := kvsm.NewKinesisVideoServer(kvsm.WithBlockTime(blockTime))
	defer server.Close()

	pro := newProvider(t, server)

	ch := make(chan *kvm.BlockWithBaseTimecode)
	timecodes := []uint64{
		1000,
		10001,
	}
	go func() {
		defer close(ch)
		for _, tc := range timecodes {
			ch <- &kvm.BlockWithBaseTimecode{
				Timecode: tc,
				Block:    newBlock(0),
			}
		}
	}()

	chResp := make(chan kvm.FragmentEvent)
	go func() {
		for range chResp {
		}
	}()

	// Cause timeout error
	client := http.Client{
		Timeout: blockTime / 2,
	}
	var err error
	pro.PutMedia(ch, chResp,
		kvm.WithHttpClient(client),
		kvm.OnError(func(e error) { err = e }),
	)
	if nerr, ok := err.(net.Error); !ok || !nerr.Timeout() {
		t.Fatalf("Err must be timeout error but %v", err)
	}
}

func newProvider(t *testing.T, server *kvsm.KinesisVideoServer) *kvm.Provider {
	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := kvm.New(session.Must(session.NewSession(cfg)), cfg)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	pro, err := cli.Provider(kvm.StreamName("test-stream"), []kvm.TrackEntry{})
	if err != nil {
		t.Fatalf("Failed to create new provider: %v", err)
	}
	return pro
}

func newBlock(timecode int16) ebml.Block {
	return ebml.Block{
		TrackNumber: 1,
		Timecode:    timecode,
		Keyframe:    false,
		Invisible:   false,
		Data:        testData,
	}
}
func newTags(tags []kvm.SimpleTag) kvsm.TagsTest {
	return kvsm.TagsTest{Tag: []kvm.Tag{{SimpleTag: tags}}}
}
