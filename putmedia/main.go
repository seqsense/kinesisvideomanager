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

	chBlockChWithBaseTime := make(chan *kvm.BlockChWithBaseTime)
	go func() {
		start := time.Now()
		donePrevious := make(chan struct{})
		close(donePrevious)
		for {
			tBase := uint64(time.Now().Sub(start))/1000000 + 1000
			ch := make(chan ebml.Block)
			chTag := make(chan kvm.Tag)
			close(chTag)
			chBlockChWithBaseTime <- &kvm.BlockChWithBaseTime{
				Timecode: tBase,
				Block:    ch,
				Tag:      chTag,
			}
			<-donePrevious
			donePrevious = make(chan struct{})
			go func() {
				for i := 0; i < 90; i++ {
					t := uint64(time.Now().Sub(start)) / 1000000

					data := ebml.Block{
						1, int16(t - tBase), true, false, ebml.LacingNo, false,
						[][]byte{{0x30, 0x31}},
					}
					log.Printf("write: %d, %v", tBase, data)
					ch <- data
					time.Sleep(100 * time.Millisecond)
				}
				close(donePrevious)
				close(ch)
			}()
			time.Sleep(8 * time.Second)
		}
	}()
	pro.PutMedia(chBlockChWithBaseTime)
}
