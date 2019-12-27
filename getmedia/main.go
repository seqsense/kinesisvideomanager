package main

import (
	"log"

	"github.com/aws/aws-sdk-go/aws/session"

	kvm "github.com/seqsense/kinesis-test/kinesisvideomanager"
	"github.com/seqsense/sq-gst-go/appsrc"
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

	con, err := manager.Consumer(kvm.StreamName(streamName))
	if err != nil {
		log.Fatal(err)
	}

	l, err := gstlaunch.New(
		"appsrc name=src format=GST_FORMAT_TIME is-live=true do-timestamp=true" +
			" caps=video/x-vp8 ! " +
			"vp8dec ! " +
			"videoconvert ! " +
			"ximagesink")
	if err != nil {
		log.Fatal(err)
	}
	src, err := l.GetElement("src")
	if err != nil {
		log.Fatal(err)
	}
	as := appsrc.New(src)
	defer as.EOS()
	l.Start()

	ch := make(chan *kvm.BlockWithBaseTimecode)
	chTag := make(chan *kvm.Tag)
	go func() {
		for {
			select {
			case tag := <-chTag:
				for _, t := range tag.SimpleTag {
					log.Printf("tag: %s: %s", t.TagName, t.TagString)
				}
			case c, ok := <-ch:
				if !ok {
					return
				}
				// log.Printf("block: %v", c)
				as.PushBuffer(c.Block.Data[0])
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
