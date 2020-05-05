package kinesisvideomanager

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
)

func TestConsumer(t *testing.T) {
	server := NewKinesisVideoServer()
	defer server.Close()

	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := New(session.Must(session.NewSession(cfg)), cfg)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	con, err := cli.Consumer(StreamName("test-stream"))
	if err != nil {
		t.Fatalf("Failed to create new consumer: %v", err)
	}

	testData := []FragmentTest{
		{
			Cluster: ClusterTest{
				Timecode:    1000,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(100), newBlock(200)},
			},
			Tags: newTags([]SimpleTag{{TagName: "TEST_TAG", TagString: "1"}}),
		},
		{
			Cluster: ClusterTest{
				Timecode:    2000,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(100)},
			},
			Tags: newTags([]SimpleTag{{TagName: "TEST_TAG", TagString: "2"}}),
		},
	}
	for _, f := range testData {
		server.RegisterFragment(f)
	}

	ch := make(chan *BlockWithBaseTimecode)
	var blocks []*BlockWithBaseTimecode
	chTag := make(chan *Tag)
	var tags []SimpleTag
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		defer cancel()
		for {
			select {
			case b, ok := <-ch:
				if !ok {
					continue
				}
				blocks = append(blocks, b)
			case t, ok := <-chTag:
				if !ok {
					return
				}
				tags = append(tags, t.SimpleTag...)
			}
		}
	}()

	opts := []GetMediaOption{
		WithStartSelectorProducerTimestamp(time.Unix(1001, 0)),
	}
	_, err = con.GetMedia(ch, chTag, opts...)
	if err != nil {
		t.Fatalf("Failed to run GetMedia: %v", err)
	}

	<-ctx.Done()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("GetMedia timed out")
	}

	// check only second fragment was loaded
	expectedBlocks := []*BlockWithBaseTimecode{
		{
			Timecode: 2000,
			Block:    newBlock(0),
		},
		{
			Timecode: 2000,
			Block:    newBlock(100),
		},
	}

	if !reflect.DeepEqual(expectedBlocks, blocks) {
		t.Errorf("Unexpected Blocks\n expected:%+v\n actual%+v", expectedBlocks, blocks)
	}

	expectedTags := []SimpleTag{
		{TagName: "TEST_TAG", TagString: "2"},
	}
	if !reflect.DeepEqual(expectedTags, tags) {
		t.Errorf("Unexpected Tags\n expected:%+v\n actual%+v", expectedTags, tags)
	}
}
