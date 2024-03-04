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
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/at-wat/ebml-go"
	kvm "github.com/seqsense/kinesisvideomanager"
	"github.com/seqsense/sq-gst-go/appsink"
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
			" undershoot=50 ! " +
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

	start := time.Now()

	w, err := pro.PutMedia()
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for {
			fe, err := w.ReadResponse()
			if err != nil {
				log.Println(err)
				return
			}
			log.Printf("response: %+v", *fe)
		}
	}()

	as := appsink.New(sink, func(b []byte, n int) {
		t := uint64(time.Now().Sub(start)) / 1000000

		data := &kvm.BlockWithBaseTimecode{
			Timecode: t,
			Block: ebml.Block{
				TrackNumber: 1,
				Timecode:    0,
				Keyframe:    true,
				Lacing:      ebml.LacingNo,
				Data:        [][]byte{b},
			},
		}
		// log.Printf("write: %v", data)
		err := w.Write(data)
		if err != nil {
			log.Println(err)
		}
	})
	defer func() {
		as.Close()
		if err = w.Close(); err != nil {
			log.Println(err)
		}
	}()
	l.Start()

	select {}
}
