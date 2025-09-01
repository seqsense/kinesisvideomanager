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

package mediafragment

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/go-cmp/cmp"

	kvm "github.com/seqsense/kinesisvideomanager"
	kvsm "github.com/seqsense/kinesisvideomanager/kvsmockserver"
)

func TestListFragments(t *testing.T) {
	var serverTimestampOrigin float64 = 100
	server := kvsm.NewKinesisVideoServer(kvsm.WithTimestampOrigin(0, serverTimestampOrigin))
	defer server.Close()

	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := New(kvm.StreamName("test-stream"), session.Must(session.NewSession(cfg)))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	assertNumFragments := func(t *testing.T, num int, opt ListFragmentsOption) {
		t.Helper()
		list, err := cli.ListFragments(opt)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(list.Fragments); n != num {
			t.Fatalf("Expected %d fragments, got %d fragments", num, n)
		}
	}

	assertNumFragments(t, 0, WithServerTimestampRange(time.Unix(102, 0), time.Unix(103, 0)))
	assertNumFragments(t, 0, WithProducerTimestampRange(time.Unix(2, 0), time.Unix(3, 0)))

	testData := []kvsm.FragmentTest{
		{Cluster: kvsm.ClusterTest{Timecode: 1000}},
		{Cluster: kvsm.ClusterTest{Timecode: 2000}},
		{Cluster: kvsm.ClusterTest{Timecode: 3000}},
		{Cluster: kvsm.ClusterTest{Timecode: 4000}},
	}
	for _, f := range testData {
		server.RegisterFragment(f)
	}

	assertNumFragments(t, 2, WithServerTimestampRange(time.Unix(102, 0), time.Unix(103, 0)))
	assertNumFragments(t, 2, WithProducerTimestampRange(time.Unix(2, 0), time.Unix(3, 0)))
}

func TestGetMediaForFragmentList(t *testing.T) {
	var serverTimestampOrigin float64 = 100
	server := kvsm.NewKinesisVideoServer(kvsm.WithTimestampOrigin(0, serverTimestampOrigin))
	defer server.Close()

	cfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials("key", "secret", "token"),
		Region:      aws.String("ap-northeast-1"),
		Endpoint:    &server.URL,
	}
	cli, err := New(kvm.StreamName("test-stream"), session.Must(session.NewSession(cfg)))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	newBlock := func(timecode int16) ebml.Block {
		return ebml.Block{
			TrackNumber: 1,
			Timecode:    timecode,
			Data:        [][]byte{{0xaa, 0xbb, 0xcc}},
		}
	}
	testData := []kvsm.FragmentTest{
		{Cluster: kvsm.ClusterTest{
			Timecode:    1000,
			SimpleBlock: []ebml.Block{newBlock(0), newBlock(100)},
		}},
		{Cluster: kvsm.ClusterTest{
			Timecode:    2000,
			SimpleBlock: []ebml.Block{newBlock(10), newBlock(110)},
		}},
		{Cluster: kvsm.ClusterTest{
			Timecode:    3000,
			SimpleBlock: []ebml.Block{newBlock(20), newBlock(120)},
		}},
		{Cluster: kvsm.ClusterTest{
			Timecode:    4000,
			SimpleBlock: []ebml.Block{newBlock(30), newBlock(130)},
		}},
	}
	for _, f := range testData {
		server.RegisterFragment(f)
	}

	var blocks []kvm.BlockWithBaseTimecode
	if err := cli.GetMediaForFragmentList(
		NewFragmentIDs(kvsm.FragmentNumberFromTimecode(2000), kvsm.FragmentNumberFromTimecode(3000)),
		func(f kvm.Fragment) {
			for _, b := range f {
				blocks = append(blocks, *b.BlockWithBaseTimecode)
			}
		}, func(err error) {
			t.Error(err)
		}); err != nil {
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			// HTTP response reader returns EOF to the successful read with data
			// and ebml-go return unexpected EOF. Temporary ignore unexpected EOF error.
			// https://github.com/at-wat/ebml-go/issues/193
			t.Error(err)
		}
	}

	expectedBlocks := []kvm.BlockWithBaseTimecode{
		{Timecode: 2000, Block: newBlock(10)},
		{Timecode: 2000, Block: newBlock(110)},
		{Timecode: 3000, Block: newBlock(20)},
		{Timecode: 3000, Block: newBlock(120)},
	}

	if diff := cmp.Diff(expectedBlocks, blocks); diff != "" {
		t.Errorf("Unexpected blocks: %s", diff)
	}
}
