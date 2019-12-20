package main

import (
	"io"
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

	start := time.Now()

	for {
		ch := make(chan *kvm.Cluster, 10)
		go func() {
			for i := 0; i < 5; i++ {
				t := time.Now().Sub(start)
				data := &kvm.Cluster{
					Timecode: uint64(t) / 1000000,
					Position: 0,
					SimpleBlock: []ebml.Block{
						{1, 10, true, false, ebml.LacingNo, false, [][]byte{{0x30, 0x31}}},
					},
				}
				log.Printf("write: %v", *data)
				ch <- data
				time.Sleep(200 * time.Millisecond)
			}
			close(ch)
		}()

		res, err := pro.PutMedia(ch)
		if err != nil {
			log.Fatal(err)
		}
		for {
			b := make([]byte, 4096)
			n, err := res.Read(b)
			if err == io.EOF {
				res.Close()
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("response: %s", string(b[:n]))
		}
	}
}
