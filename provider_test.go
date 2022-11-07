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
	"bytes"
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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"

	kvm "github.com/seqsense/kinesisvideomanager"
	kvsm "github.com/seqsense/kinesisvideomanager/kvsmockserver"
)

var testData = [][]byte{{0x01, 0x02}}

const fragmentEventFmt = `{"EventType":"ERROR","FragmentTimecode":%d,"FragmentNumber":"91343852333754009371412493862204112772176002064","ErrorId":5000,"ErrorCode":"DUMMY_ERROR"}`

func TestProvider(t *testing.T) {
	var mu sync.Mutex

	retryOpt := kvm.WithPutMediaRetry(2, 100*time.Millisecond)

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
		errCheck       func(*testing.T, int, error) bool
		timeout        time.Duration
	}{
		"NoError": {
			mockServerOpts: func(*testing.T, map[uint64]bool, *bool, func()) []kvsm.KinesisVideoServerOption { return nil },
			expected:       []kvsm.FragmentTest{expected0, expected1, expected2},
		},
		"HTTPErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
						mu.Lock()
						defer mu.Unlock()
						if !dropped[timecode] {
							dropped[timecode] = true
							w.WriteHeader(500)
							if _, err := w.Write([]byte("Dummy error")); err != nil {
								t.Error(err)
							}
							t.Logf("HTTP error injected: timecode=%d", timecode)
							return false
						}
						return true
					}),
				}
			},
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected1, expected2},
			timeout:      2 * time.Second,
		},
		"DelayedHTTPErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
						mu.Lock()
						defer mu.Unlock()
						if !dropped[timecode] {
							time.Sleep(75 * time.Millisecond)
							dropped[timecode] = true
							w.WriteHeader(500)
							if _, err := w.Write([]byte("Dummy error")); err != nil {
								t.Error(err)
							}
							t.Logf("HTTP error injected: timecode=%d", timecode)
							return false
						}
						return true
					}),
				}
			},
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected1, expected2},
		},
		"KinesisErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
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
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected0, expected1, expected1, expected2, expected2},
		},
		"KinesisFailDumpShort": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
						if _, err := w.Write([]byte(fmt.Sprintf(fragmentEventFmt, timecode))); err != nil {
							t.Error(err)
						}
						t.Logf("Kinesis error injected: timecode=%d", timecode)
						return false
					}),
				}
			},
			putMediaOpts: []kvm.PutMediaOption{
				retryOpt,
				kvm.WithFragmentHeadDumpLen(17),
				kvm.WithSegmentUID([]byte{0x00, 0x01, 0x02, 0x03}),
			},
			expected: []kvsm.FragmentTest{},
			errCheck: func(t *testing.T, cnt int, err error) bool {
				if err == nil {
					t.Error("Expected error")
					return false
				}
				if cnt != 2 {
					// Skip first fragment.
					return false
				}
				fe, ok := err.(*kvm.FragmentEventError)
				if !ok {
					t.Errorf("Expected FragmentEventError, got %T", err)
					return false
				}
				expectedDump := []byte{
					0x1a, 0x45, 0xdf, 0xa3, 0xa4, // EBML
					0x42, 0x86, 0x81, 0x01, // EBMLVersion
					0x42, 0xf7, 0x81, 0x01, // ReadVersion
					0x42, 0xf2, 0x81, 0x04, // MaxIDLength
				}
				if dump := fe.Dump(); !bytes.Equal(expectedDump, dump) {
					t.Errorf("Expected dump:\n%v\ngot:\n%v", expectedDump, dump)
				}
				return true
			},
		},
		"KinesisFailDump": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
						if _, err := w.Write([]byte(fmt.Sprintf(fragmentEventFmt, timecode))); err != nil {
							t.Error(err)
						}
						t.Logf("Kinesis error injected: timecode=%d", timecode)
						return false
					}),
				}
			},
			putMediaOpts: []kvm.PutMediaOption{
				retryOpt,
				kvm.WithFragmentHeadDumpLen(512),
				kvm.WithSegmentUID([]byte{0x00, 0x01, 0x02, 0x03}),
			},
			expected: []kvsm.FragmentTest{},
			errCheck: func(t *testing.T, cnt int, err error) bool {
				if err == nil {
					t.Error("Expected error")
					return false
				}
				if cnt != 2 {
					// Skip first fragment.
					return false
				}
				fe, ok := err.(*kvm.FragmentEventError)
				if !ok {
					t.Errorf("Expected FragmentEventError, got %T", err)
					return false
				}
				expectedDump := []byte{
					0x1a, 0x45, 0xdf, 0xa3, 0xa4, // EBML
					0x42, 0x86, 0x81, 0x01, // EBMLVersion
					0x42, 0xf7, 0x81, 0x01, // ReadVersion
					0x42, 0xf2, 0x81, 0x04, // MaxIDLength
					0x42, 0xf3, 0x81, 0x08, // MaxSizeLength
					0x42, 0x82, 0x89, 0x6d, 0x61, 0x74, 0x72, 0x6f, 0x73, 0x6b, 0x61, 0x00, // DocType
					0x42, 0x87, 0x81, 0x02, // DocTypeVersion
					0x42, 0x85, 0x81, 0x02, // DocTypeReadVersion
					0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // Segment
					0x15, 0x49, 0xa9, 0x66, 0xf2, // Info
					0x2a, 0xd7, 0xb1, 0x83, 0x0f, 0x42, 0x40, // TimecodeScale
					0x73, 0xa4, 0x84, 0x00, 0x01, 0x02, 0x03, // SegmentUID
					0x73, 0x84, 0x81, 0x00, // SegmentFilename
					0x7b, 0xa9, 0x9d, 0x6b, 0x69, 0x6e, 0x65, 0x73, 0x69, 0x73, 0x76, 0x69, 0x64, 0x65, 0x6f, 0x6d, 0x61, 0x6e, 0x61, 0x67, 0x65, 0x72, 0x2e, 0x50, 0x72, 0x6f, 0x76, 0x69, 0x64, 0x65, 0x72, 0x00, // Title
					0x4d, 0x80, 0x9d, 0x6b, 0x69, 0x6e, 0x65, 0x73, 0x69, 0x73, 0x76, 0x69, 0x64, 0x65, 0x6f, 0x6d, 0x61, 0x6e, 0x61, 0x67, 0x65, 0x72, 0x2e, 0x50, 0x72, 0x6f, 0x76, 0x69, 0x64, 0x65, 0x72, 0x00, // MuxingApp
					0x57, 0x41, 0x9d, 0x6b, 0x69, 0x6e, 0x65, 0x73, 0x69, 0x73, 0x76, 0x69, 0x64, 0x65, 0x6f, 0x6d, 0x61, 0x6e, 0x61, 0x67, 0x65, 0x72, 0x2e, 0x50, 0x72, 0x6f, 0x76, 0x69, 0x64, 0x65, 0x72, 0x00, // WritingApp
					0x16, 0x54, 0xae, 0x6b, 0x80, // Tracks
					0x1f, 0x43, 0xb6, 0x75, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // Cluster
					0xe7, 0x82, 0x00, 0x00, // Timecode
					0xa3, 0x86, 0x81, 0x00, 0x00, 0x00, 0x01, 0x02, // SimpleBlock
					0xa3, 0x86, 0x81, 0x00, 0x01, 0x00, 0x01, 0x02, // SimpleBlock
					0x12, 0x54, 0xc3, 0x67, 0x97, // Tags
					0x73, 0x73, 0x94, // Tag
					0x67, 0xc8, 0x91, // SimpleTag
					0x45, 0xa3, 0x89, 0x54, 0x45, 0x53, 0x54, 0x5f, 0x54, 0x41, 0x47, 0x00, // TagName
					0x44, 0x87, 0x82, 0x32, 0x00, // TagString
				}
				dump := fe.Dump()
				dump[191], dump[192] = 0, 0 // clear Timecode
				if !bytes.Equal(expectedDump, dump) {
					t.Errorf("Expected dump:\n%v\ngot:\n%v", expectedDump, dump)
				}
				return true
			},
		},
		"DelayedKinesisErrorRetry": {
			mockServerOpts: func(t *testing.T, dropped map[uint64]bool, _ *bool, _ func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
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
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected0, expected1, expected1, expected2, expected2},
		},
		"DisconnectRetry": {
			mockServerOpts: func(t *testing.T, _ map[uint64]bool, disconnected *bool, disconnect func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
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
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected1, expected2},
		},
		"DelayedDisconnectRetry": {
			mockServerOpts: func(t *testing.T, _ map[uint64]bool, disconnected *bool, disconnect func()) []kvsm.KinesisVideoServerOption {
				return []kvsm.KinesisVideoServerOption{
					kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
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
			putMediaOpts: []kvm.PutMediaOption{retryOpt},
			expected:     []kvsm.FragmentTest{expected0, expected1, expected2},
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
			timeout := testCase.timeout
			if timeout == 0 {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			t.Log("test case start")

			var cntErr, cntTag uint32
			var skipBelow uint32
			opts := []kvm.PutMediaOption{
				kvm.WithFragmentTimecodeType(kvm.FragmentTimecodeTypeRelative),
				kvm.WithProducerStartTimestamp(startTimestamp),
				kvm.WithTags(func() []kvm.SimpleTag {
					cnt := atomic.AddUint32(&cntTag, 1)
					return []kvm.SimpleTag{
						{TagName: "TEST_TAG", TagString: fmt.Sprintf("%d", cnt)},
					}
				}),
				kvm.OnError(func(e error) {
					if testCase.errCheck != nil {
						cnt := atomic.AddUint32(&cntErr, 1)
						if testCase.errCheck(t, int(cnt), e) {
							atomic.StoreUint32(&skipBelow, 1)
						}
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
				defer func() {
					cancel()
					wg.Done()
				}()
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
			if err := w.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}

			wg.Wait()
			if skipBelow == 1 {
				return
			}

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

	timecodes := []uint64{
		1000,
		10001,
	}

	// Cause timeout error
	client := http.Client{
		Timeout: blockTime / 2,
	}
	var errBg error
	w, err := pro.PutMedia(
		kvm.WithHttpClient(client),
		kvm.OnError(func(e error) { errBg = e }),
	)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, err := w.ReadResponse(); err != nil {
				return
			}
		}
	}()
	for _, tc := range timecodes {
		if err := w.Write(&kvm.BlockWithBaseTimecode{
			Timecode: tc,
			Block:    newBlock(0),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	var netErr net.Error
	if !errors.As(errBg, &netErr) || !netErr.Timeout() {
		t.Fatalf("Err must be timeout error but %v", errBg)
	}
}

func TestProvider_WithPutMediaLogger(t *testing.T) {
	server := kvsm.NewKinesisVideoServer(
		kvsm.WithPutMediaHook(func(timecode uint64, f *kvsm.FragmentTest, w http.ResponseWriter) bool {
			if _, err := w.Write([]byte(
				fmt.Sprintf(fragmentEventFmt, timecode),
			)); err != nil {
				t.Error(err)
			}
			t.Logf("Kinesis error injected: timecode=%d", timecode)
			return false
		}),
	)
	defer server.Close()

	pro := newProvider(t, server)

	var logger dummyWarnfLogger
	w, err := pro.PutMedia(
		kvm.WithPutMediaLogger(&logger),
		kvm.OnError(func(error) {}),
		kvm.WithConnectionTimeout(time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			if _, err := w.ReadResponse(); err != nil {
				return
			}
		}
	}()
	time.Sleep(10 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	expected := `Receiving block timed out, clean connections: { StreamID: "test-stream" }`
	if expected != logger.lastErr {
		t.Errorf("Expected log: '%s', got: '%s'", expected, logger.lastErr)
	}
}

func TestProvider_shutdownTwice(t *testing.T) {
	server := kvsm.NewKinesisVideoServer()
	defer server.Close()

	pro := newProvider(t, server)

	w, err := pro.PutMedia()
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			if _, err := w.ReadResponse(); err != nil {
				return
			}
		}
	}()
	time.Sleep(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := w.Shutdown(ctx); err != context.Canceled {
		t.Fatalf("Expected error: %v, got: %v", context.Canceled, err)
	}
	if err := w.Shutdown(ctx); err != context.Canceled {
		t.Fatalf("Expected error: %v, got: %v", context.Canceled, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
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

type dummyWarnfLogger struct {
	kvm.LoggerIF

	lastErr string
}

func (l *dummyWarnfLogger) Warn(args ...interface{}) {
	l.lastErr = fmt.Sprint(args...)
}

func (l *dummyWarnfLogger) Warnf(format string, args ...interface{}) {
	l.lastErr = fmt.Sprintf(format, args...)
}

func (l *dummyWarnfLogger) Debugf(format string, args ...interface{}) {
}
