package main

import (
	"log"

	"github.com/aws/aws-sdk-go/aws/session"

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

	con, err := manager.Consumer(kvm.StreamName(streamName))
	if err != nil {
		log.Fatal(err)
	}

	ch := make(chan *kvm.BlockWithBaseTimecode)
	chTag := make(chan kvm.Tag)
	go func() {
		for {
			select {
			case tag := <-chTag:
				log.Printf("tag: %v", tag)
			case c, ok := <-ch:
				if !ok {
					return
				}
				log.Printf("block: %v", c)
			}
		}
	}()
	data, err := con.GetMedia(ch, chTag)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("container: %v", data)
	close(ch)
}
