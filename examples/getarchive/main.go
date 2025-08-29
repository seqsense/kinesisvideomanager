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

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"

	kvm "github.com/seqsense/kinesisvideomanager"
	"github.com/seqsense/kinesisvideomanager/mediafragment"
	"github.com/seqsense/sq-gst-go/appsrc"
	"github.com/seqsense/sq-gst-go/gstlaunch"
)

const (
	defaultStreamName = "test-stream"
)

func main() {
	log.SetFlags(log.Lmicroseconds)

	streamName := defaultStreamName
	if len(os.Args) > 1 {
		streamName = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	cli, err := mediafragment.New(ctx, kvm.StreamName(streamName), cfg)
	if err != nil {
		log.Fatal(err)
	}

	list, err := cli.ListFragments(ctx, mediafragment.WithServerTimestampRange(time.Now().Add(-time.Hour), time.Now()))
	if err != nil {
		log.Fatal(err)
	}
	list.Sort()
	list.Uniq()

	if list.Len() == 0 {
		log.Fatal("No fragments are found in the last 1 hour. Run putmedia example first to store some fragments.")
	}

	for _, frag := range list.Fragments {
		log.Printf("fragment: %+v", frag)
	}

	l, err := gstlaunch.New(
		"appsrc name=src format=GST_FORMAT_TIME is-live=true do-timestamp=true" +
			" caps=video/x-vp8 ! " +
			"vp8dec ! " +
			"videoconvert ! " +
			"ximagesink")
	if err != nil {
		log.Fatal(err)
	}
	src, err := l.GetElement("src")
	if err != nil {
		log.Fatal(err)
	}
	as := appsrc.New(src)
	defer as.EOS()
	l.Start()

	var lastTimecode int64
	// Use context without deadline
	if err := cli.GetMediaForFragmentList(context.Background(), list.FragmentIDs(), func(f kvm.Fragment) {
		for _, b := range f {
			timecode := b.AbsTimecode()
			if lastTimecode == 0 || timecode < lastTimecode {
				lastTimecode = b.AbsTimecode()
			}
			tDiff := timecode - lastTimecode
			lastTimecode = timecode
			as.PushBuffer(b.Block.Data[0])
			time.Sleep(time.Duration(tDiff) * time.Millisecond)
		}
	}, func(err error) {
		log.Print(err)
	}); err != nil {
		log.Fatal(err)
	}
}
