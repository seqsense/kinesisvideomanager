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
		"pulsesrc ! " +
			"audio/x-raw,rate=48000,channels=1,format=S16LE !" +
			"queue max-size-buffers=5 ! " +
			"opusenc bitrate=32000 bitrate-type=vbr frame-size=40 ! " +
			"appsink name=sink",
	)
	if err != nil {
		log.Fatal(err)
	}
	sink, err := l.GetElement("sink")
	if err != nil {
		log.Fatal(err)
	}

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

	pro.PutMedia(ch, chTag)
}
