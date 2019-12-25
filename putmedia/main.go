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

	for i := uint64(0); ; i += 10000 {
		start := time.Now()
		ch := make(chan ebml.Block, 10)
		chTag := make(chan kvm.Tag)
		chClosed := make(chan struct{})
		go func() {
			close(chTag)
			go func() {
				for i := 0; i < 100; i++ {
					t := time.Now().Sub(start)
					data := ebml.Block{
						1, int16(uint64(t) / 1000000), true, false, ebml.LacingNo, false,
						[][]byte{{0x30, 0x31}},
					}
					log.Printf("write: %v", data)
					ch <- data
					time.Sleep(100 * time.Millisecond)
				}
				close(ch)
				close(chClosed)
			}()
			res, err := pro.PutMedia(i, ch, chTag)
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
		}()
		<-chClosed
	}
}
