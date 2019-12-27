package main

import (
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/at-wat/ebml-go"
	kvm "github.com/seqsense/kinesis-test/kinesisvideomanager"
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

	pro, err := manager.Provider(kvm.StreamName(streamName))
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
	chTag := make(chan *kvm.Tag, 10)
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

	err = pro.PutMedia(ch, chTag, chResp)
	if err != nil {
		log.Printf("failed: %v", err)
	}
}
