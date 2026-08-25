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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	kvm "github.com/seqsense/kinesisvideomanager/v2"
	kvsm "github.com/seqsense/kinesisvideomanager/v2/kvsmockserver"
)

var testData = [][]byte{{0x01, 0x02}}

const fragmentEventFmt = `{"EventType":"ERROR","FragmentTimecode":%d,"FragmentNumber":"91343852333754009371412493862204112772176002064","ErrorId":5000,"ErrorCode":"DUMMY_ERROR"}`

func TestProvider(t *testing.T) {
	var mu sync.Mutex

	startTimestamp := time.Now()
	startTimestampInMillis := uint64(startTimestamp.UnixNano() / int64(time.Millisecond))

	expected0 := kvsm.FragmentTest{
		Cluster: kvsm.ClusterTest{
			Timecode:    startTimestampInMillis + 1000,
			SimpleBlock: []ebml.Block{newBlock(0), newBlock(8000), newBlock(9000)},
		},
		Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "1"}}),
	}
	expected1 := kvsm.FragmentTest{
		Cluster: kvsm.ClusterTest{
			Timecode:    startTimestampInMillis + 10001,
			SimpleBlock: []ebml.Block{newBlock(0), newBlock(1)},
		},
		Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "2"}}),
	}
	expected2 := kvsm.FragmentTest{
		Cluster: kvsm.ClusterTest{
			Timecode:    startTimestampInMillis + 43000,
			SimpleBlock: []ebml.Block{newBlock(0), newBlock(1)},
		},
		Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "3"}}),
	}

	testCases := map[string]struct {
		mockServerOpts func(*testing.T, map[uint64]bool, *bool, func()) []kvsm.KinesisVideoServerOption
		putMediaOpts   []kvm.PutMediaOption
		expected       []kvsm.FragmentTest
	}{
		"NoError": {
			mockServerOpts: func(*testing.T, map[uint64]bool, *bool, func()) []kvsm.KinesisVideoServerOption { return nil },
			expected:       []kvsm.FragmentTest{expected0, expected1, expected2},
		},
		"KinesisErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w io.Writer) bool {
						mu.Lock()
						defer mu.Unlock()
						if !dropped[timecode] {
							dropped[timecode] = true
							if _, err := w.Write([]byte(fmt.Sprintf(fragmentEventFmt, timecode))); err != nil {
								t.Error(err)
							}
							t.Logf("Kinesis error injected: timecode=%d", timecode)
							return false
						}
						return true
					}),
				}
			},
			expected: []kvsm.FragmentTest{expected0, expected0, expected1, expected1, expected2, expected2},
		},
		"DelayedKinesisErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w io.Writer) bool {
						mu.Lock()
						defer mu.Unlock()
						if !dropped[timecode] {
							time.Sleep(75 * time.Millisecond)
							dropped[timecode] = true
							if _, err := w.Write([]byte(fmt.Sprintf(fragmentEventFmt, timecode))); err != nil {
								t.Error(err)
							}
							t.Logf("Kinesis error injected: timecode=%d", timecode)
							return false
						}
						return true
					}),
				}
			},
			expected: []kvsm.FragmentTest{expected0, expected0, expected1, expected1, expected2, expected2},
		},
		"DisconnectRetry": {
			mockServerOpts: func(t *testing.T, _ map[uint64]bool, disconnected *bool, disconnect func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w io.Writer) bool {
						mu.Lock()
						defer mu.Unlock()
						if !*disconnected {
							*disconnected = true
							t.Logf("Disconnect injected: timecode=%d", timecode)
							disconnect()
							return false
						}
						return true
					}),
				}
			},
			expected: []kvsm.FragmentTest{expected0, expected1, expected2},
		},
		"DelayedDisconnectRetry": {
			mockServerOpts: func(t *testing.T, _ map[uint64]bool, disconnected *bool, disconnect func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w io.Writer) bool {
						mu.Lock()
						defer mu.Unlock()
						if !*disconnected {
							time.Sleep(75 * time.Millisecond)
							*disconnected = true
							t.Logf("Disconnect injected: timecode=%d", timecode)
							disconnect()
							return false
						}
						return true
					}),
				}
			},
			expected: []kvsm.FragmentTest{expected0, expected1, expected2},
		},
	}

	for name, testCase := range testCases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			dropped := make(map[uint64]bool)
			var disconnected bool

			var server *kvsm.KinesisVideoServer
			server = kvsm.NewKinesisVideoServer(testCase.mockServerOpts(
				t, dropped, &disconnected, func() {
					server.CloseClientConnections()
				},
			)...)
			defer server.Close()

			pro := newProvider(t, server)

			timecodes := []uint64{
				1000,
				9000,
				10000,
				10001, // switch to the next fragment here
				10002,
				43000, // force next fragment (jump >32768)
				43001,
			}

			var response []kvm.FragmentEvent
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)

			var cntTag uint32
			opts := []kvm.PutMediaOption{
				kvm.WithFragmentTimecodeType(kvm.FragmentTimecodeTypeRelative),
				kvm.WithProducerStartTimestamp(startTimestamp),
				kvm.WithTags(func() []kvm.SimpleTag {
					cnt := atomic.AddUint32(&cntTag, 1)
					return []kvm.SimpleTag{
						{TagName: "TEST_TAG", TagString: fmt.Sprintf("%d", cnt)},
					}
				}),
			}
			opts = append(opts, testCase.putMediaOpts...)
			w, err := pro.PutMedia(opts...)
			if err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					resp, err := w.ReadResponse()
					if err != nil {
						if err != io.EOF {
							t.Error(err)
						}
						return
					}
					response = append(response, *resp)
				}
			}()

			for _, tc := range timecodes {
				if err := w.Write(&kvm.BlockWithBaseTimecode{
					Timecode: tc,
					Block:    newBlock(0),
				}); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			wg.Wait()
			cancel()

			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("PutMedia timed out")
			}

			if n := len(response); n != len(testCase.expected) {
				t.Fatalf(
					"Response size expected to be %d but %d\nresponse: %v",
					len(testCase.expected), n,
					response,
				)
			}

			for _, fragment := range testCase.expected {
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
	var wg sync.WaitGroup
	defer wg.Wait()

	blockTime := 2 * time.Second
	server := kvsm.NewKinesisVideoServer(kvsm.WithBlockTime(blockTime))
	defer server.Close()

	pro := newProvider(t, server)

	// Cause timeout error
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: time.Nanosecond,
			}).DialContext,
		},
	}
	_, err := pro.PutMedia(
		kvm.WithHttpClient(client),
	)
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("Err must be timeout error but %v", err)
	}
}

func newProvider(t *testing.T, server *kvsm.KinesisVideoServer) *kvm.Provider {
	cfg := aws.Config{
		Credentials:  credentials.NewStaticCredentialsProvider("key", "secret", "token"),
		Region:       "ap-northeast-1",
		BaseEndpoint: &server.URL,
	}
	cli, err := kvm.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	pro, err := cli.Provider(ctx, kvm.StreamName("test-stream"), []kvm.TrackEntry{})
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

type dummyDebugfLogger struct {
	kvm.LoggerIF

	lastErr string
}

func (l *dummyDebugfLogger) Debug(args ...interface{}) {
	l.lastErr = fmt.Sprint(args...)
}

func (l *dummyDebugfLogger) Debugf(format string, args ...interface{}) {
	l.lastErr = fmt.Sprintf(format, args...)
}
