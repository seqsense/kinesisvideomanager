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

	for {
		ch := make(chan *kvm.Cluster)
		go func() {
			for {
				c, ok := <-ch
				if !ok {
					return
				}
				log.Printf("cluster: %v", *c)
			}
		}()
		data, err := con.GetMedia(ch)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("container: %v", *data)
		close(ch)
	}
}
