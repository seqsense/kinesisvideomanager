package kinesisvideomanager

import (
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
)

var testData = [][]byte{{0x01, 0x02}}

func TestProvider(t *testing.T) {
	server := NewKinesisVideoServer()
	defer server.Close()

	pro := newProvider(t, server)

	ch := make(chan *BlockWithBaseTimecode)
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
			ch <- &BlockWithBaseTimecode{
				Timecode: tc,
				Block:    newBlock(0),
			}
		}
	}()

	chResp := make(chan FragmentEvent)
	var response []FragmentEvent
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
	opts := []PutMediaOption{
		WithFragmentTimecodeType(FragmentTimecodeTypeRelative),
		WithProducerStartTimestamp(startTimestamp),
		WithTags(func() []SimpleTag {
			cnt++
			return []SimpleTag{
				{TagName: "TEST_TAG", TagString: fmt.Sprintf("%d", cnt)},
			}
		}),
	}
	if err := pro.PutMedia(ch, chResp, opts...); err != nil && err != io.EOF {
		t.Fatalf("Failed to run PutMedia: %v", err)
	}

	<-ctx.Done()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("PutMedia timed out")
	}

	expected := []FragmentTest{
		{
			Cluster: ClusterTest{
				Timecode:    startTimestampInMillis + 1000,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(8000), newBlock(9000)},
			},
			Tags: newTags([]SimpleTag{{TagName: "TEST_TAG", TagString: "1"}}),
		},
		{
			Cluster: ClusterTest{
				Timecode:    startTimestampInMillis + 10001,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(1)},
			},
			Tags: newTags([]SimpleTag{{TagName: "TEST_TAG", TagString: "2"}}),
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
}

func TestProvider_WithRequestTimeout(t *testing.T) {
	blockTime := 2 * time.Second
	server := NewKinesisVideoServer(WithBlockTime(blockTime))
	defer server.Close()

	pro := newProvider(t, server)

	ch := make(chan *BlockWithBaseTimecode)
	timecodes := []uint64{
		1000,
		2000,
	}
	go func() {
		defer close(ch)
		for _, tc := range timecodes {
			ch <- &BlockWithBaseTimecode{
				Timecode: tc,
				Block:    newBlock(0),
			}
		}
	}()

	chResp := make(chan FragmentEvent)
	go func() {
		for {
			select {
			case _, ok := <-chResp:
				if !ok {
					return
				}
			}
		}
	}()

	err := pro.PutMedia(ch, chResp, WithRequestTimeout(blockTime/2))
	errs := err.(multiErrors)
	if len(errs) != 1 {
		t.Fatalf("Unexoected err must be timeout error but %v", err)
	}
	if nerr, ok := errs[0].(net.Error); !ok || !nerr.Timeout() {
		t.Fatalf("Err must be timeout error but %v", err)
	}
}

func newProvider(t *testing.T, server *KinesisVideoServer) *Provider {
	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := New(session.Must(session.NewSession(cfg)), cfg)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	pro, err := cli.Provider(StreamName("test-stream"), []TrackEntry{})
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
func newTags(tags []SimpleTag) TagsTest {
	return TagsTest{Tag: []Tag{{SimpleTag: tags}}}
}
