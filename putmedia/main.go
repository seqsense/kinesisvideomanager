package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"

	"github.com/at-wat/ebml-go"
)

const (
	streamName = "test-stream"
)

func main() {
	sess := session.Must(session.NewSession())

	config := aws.NewConfig()
	kv := kinesisvideo.New(sess, config)
	cliConfig := sess.ClientConfig("kinesisvideo")

	ep, err := kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String("PUT_MEDIA"),
			StreamName: aws.String(streamName),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("endpoint: %s", *ep.DataEndpoint)

	cred, err := cliConfig.Config.Credentials.Get()
	if err != nil {
		log.Fatal(err)
	}

	signer := v4.NewSigner(
		credentials.NewStaticCredentials(cred.AccessKeyID, cred.SecretAccessKey, ""),
	)

	type EBMLHeader struct {
		EBMLVersion            uint64
		EBMLReadVersion        uint64
		EBMLMaxIDLength        uint64
		EBMLMaxSizeLength      uint64
		EBMLDocType            string
		EBMLDocTypeVersion     uint64
		EBMLDocTypeReadVersion uint64
	}
	type Info struct {
		TimecodeScale   uint64
		SegmentUID      []byte
		SegmentFilename string
		Title           string
		MuxingApp       string
		WritingApp      string
	}
	type TrackEntry struct {
		Name        string
		TrackNumber uint64
		TrackUID    uint64
		CodecID     string
		CodecName   string
		TrackType   uint64
	}
	type Tracks struct {
		TrackEntry []TrackEntry
	}
	type Cluster struct {
		Timecode    uint64
		Position    uint64
		SimpleBlock []ebml.Block
	}
	type SimpleTag struct {
		TagName   string
		TagString string `ebml:",omitempty"`
		TagBinary string `ebml:",omitempty"`
	}
	type Tag struct {
		SimpleTag []SimpleTag
	}
	type Tags struct {
		Tag []Tag
	}
	type Segment struct {
		Info    Info
		Tracks  Tracks
		Cluster []Cluster `ebml:",size=unknown"`
		Tags    []Tags
	}

	data := struct {
		Header  EBMLHeader `ebml:"EBML"`
		Segment Segment    `ebml:",size=unknown"`
	}{
		Header: EBMLHeader{
			EBMLVersion:            1,
			EBMLReadVersion:        1,
			EBMLMaxIDLength:        4,
			EBMLMaxSizeLength:      8,
			EBMLDocType:            "matroska",
			EBMLDocTypeVersion:     2,
			EBMLDocTypeReadVersion: 2,
		},
		Segment: Segment{
			Info: Info{
				SegmentUID:    []byte{0x4d, 0xe9, 0x96, 0x8a, 0x3f, 0x22, 0xea, 0x11, 0x6f, 0x88, 0xc3, 0xbc, 0x96, 0x42, 0x51, 0xdc},
				TimecodeScale: 1000000,
				Title:         "TestApp",
				MuxingApp:     "TestApp",
				WritingApp:    "TestApp",
			},
			Tracks: Tracks{
				TrackEntry: []TrackEntry{
					{
						TrackNumber: 1,
						TrackUID:    123,
						TrackType:   1,
						CodecID:     "X_TEST",
						Name:        "test_track",
					},
				},
			},
			Cluster: []Cluster{
				{
					Timecode: 11,
					Position: 0,
					SimpleBlock: []ebml.Block{
						{1, 100, true, false, ebml.LacingNo, false, [][]byte{{0x30, 0x31}}},
						{1, 110, true, false, ebml.LacingNo, false, [][]byte{{0x30, 0x31}}},
					},
				},
			},
			Tags: []Tags{
				{
					Tag: []Tag{
						{
							SimpleTag: []SimpleTag{
								{
									TagName:   "TestTag",
									TagString: "test string",
								},
							},
						},
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := ebml.Marshal(&data, &buf); err != nil {
		log.Fatal(err)
	}

	epFull := *ep.DataEndpoint + "/putMedia"
	bodyReader := bytes.NewReader(buf.Bytes())

	req, err := http.NewRequest("POST", epFull, bodyReader)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("x-amzn-stream-name", streamName)
	req.Header.Set("x-amzn-fragment-timecode-type", "RELATIVE")
	req.Header.Set("x-amzn-producer-start-timestamp", "0")
	req.Header.Set("X-Amz-Security-Token", cred.SessionToken)

	_, err = signer.Presign(
		req, bytes.NewReader([]byte{}),
		cliConfig.SigningName, cliConfig.SigningRegion,
		time.Hour, time.Now(),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Print("send request")
	var client http.Client
	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	log.Printf("response: %v", res.Header)

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("media: %s", string(b))
}
