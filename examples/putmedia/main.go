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

package main

import (
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/at-wat/ebml-go"
	kvm "github.com/seqsense/kinesisvideomanager"
	"github.com/seqsense/sq-gst-go/appsink"
	"github.com/seqsense/sq-gst-go/gstlaunch"
)

const (
	streamName = "test-stream"
)

func main() {
	log.SetFlags(log.Lmicroseconds)

	sess := session.Must(session.NewSession())
	manager, err := kvm.New(sess)
	if err != nil {
		log.Fatal(err)
	}

	pro, err := manager.Provider(kvm.StreamName(streamName), []kvm.TrackEntry{
		{
			TrackNumber: 1,
			TrackUID:    123,
			TrackType:   1,
			CodecID:     "X_TEST",
			Name:        "test_track",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	l, err := gstlaunch.New(
		"videotestsrc ! " +
			"video/x-raw,width=640,height=480,framerate=30/1 ! " +
			"videoconvert ! " +
			"timeoverlay" +
			" font-desc=50px" +
			" draw-outline=true ! " +
			"tee name=t ! " +
			"queue ! " +
			"vp8enc" +
			" buffer-size=65536 deadline=5" +
			" target-bitrate=200000 threads=2" +
			" overshoot=125 undershoot=50 ! " +
			"appsink name=sink " +
			"t. ! queue ! videoconvert ! ximagesink",
	)
	if err != nil {
		log.Fatal(err)
	}
	sink, err := l.GetElement("sink")
	if err != nil {
		log.Fatal(err)
	}

	chResp := make(chan kvm.FragmentEvent, 10)
	ch := make(chan *kvm.BlockWithBaseTimecode, 10)
	start := time.Now()

	as := appsink.New(sink, func(b []byte, n int) {
		t := uint64(time.Now().Sub(start)) / 1000000

		data := &kvm.BlockWithBaseTimecode{
			Timecode: t,
			Block: ebml.Block{
				1, 0, true, false, ebml.LacingNo, false,
				[][]byte{b},
			},
		}
		// log.Printf("write: %v", data)
		ch <- data
	})
	defer as.Close()
	l.Start()

	go func() {
		for {
			select {
			case fe := <-chResp:
				log.Printf("response: %+v", fe)
			}
		}
	}()

	err = pro.PutMedia(ch, chResp)
	if err != nil {
		log.Printf("failed: %v", err)
	}
}
