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

func TestConsumer(t *testing.T) {
	server := kvsm.NewKinesisVideoServer()
	defer server.Close()

	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := kvm.New(session.Must(session.NewSession(cfg)), cfg)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	con, err := cli.Consumer(kvm.StreamName("test-stream"))
	if err != nil {
		t.Fatalf("Failed to create new consumer: %v", err)
	}

	testData := []kvsm.FragmentTest{
		{
			Cluster: kvsm.ClusterTest{
				Timecode:    1000,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(100), newBlock(200)},
			},
			Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "1"}}),
		},
		{
			Cluster: kvsm.ClusterTest{
				Timecode:    2000,
				SimpleBlock: []ebml.Block{newBlock(0), newBlock(100)},
			},
			Tags: newTags([]kvm.SimpleTag{{TagName: "TEST_TAG", TagString: "2"}}),
		},
	}
	for _, f := range testData {
		server.RegisterFragment(f)
	}

	ch := make(chan *kvm.BlockWithBaseTimecode)
	var blocks []kvm.BlockWithBaseTimecode
	chTag := make(chan *kvm.Tag)
	var tags []kvm.SimpleTag
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
				blocks = append(blocks, *b)
			case t, ok := <-chTag:
				if !ok {
					return
				}
				tags = append(tags, t.SimpleTag...)
			}
		}
	}()

	opts := []kvm.GetMediaOption{
		kvm.WithStartSelectorProducerTimestamp(time.Unix(1001, 0)),
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
	expectedBlocks := []kvm.BlockWithBaseTimecode{
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
		t.Errorf("Unexpected Blocks\nexpected:\n%+v\nactual:\n%+v", expectedBlocks, blocks)
	}

	expectedTags := []kvm.SimpleTag{
		{TagName: "TEST_TAG", TagString: "2"},
	}
	if !reflect.DeepEqual(expectedTags, tags) {
		t.Errorf("Unexpected Tags\n expected:%+v\n actual%+v", expectedTags, tags)
	}
}
