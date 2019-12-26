package main

import (
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/at-wat/ebml-go"
	kvm "github.com/seqsense/kinesis-test/kinesisvideomanager"
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

	ch := make(chan *kvm.BlockWithBaseTimecode, 10)
	chTag := make(chan *kvm.Tag, 10)
	start := time.Now()
	go func() {
		for i := 0; ; i++ {
			t := uint64(time.Now().Sub(start)) / 1000000

			data := &kvm.BlockWithBaseTimecode{
				Timecode: t,
				Block: ebml.Block{
					1, 0, true, false, ebml.LacingNo, false,
					[][]byte{{0x30, 0x31}},
				},
			}
			log.Printf("write: %v", data)
			ch <- data
			time.Sleep(100 * time.Millisecond)
		}
	}()
	pro.PutMedia(ch, chTag)
}
