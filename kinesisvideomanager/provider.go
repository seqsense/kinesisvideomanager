package kinesisvideomanager

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"

	"github.com/at-wat/ebml-go"
)

type Provider struct {
	streamID  StreamID
	endpoint  string
	signer    *v4.Signer
	httpCli   http.Client
	cliConfig *client.Config
}

func (c *Client) Provider(streamID StreamID) (*Provider, error) {
	ep, err := c.kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String("PUT_MEDIA"),
			StreamName: streamID.StreamName(),
			StreamARN:  streamID.StreamARN(),
		},
	)
	if err != nil {
		return nil, err
	}
	return &Provider{
		streamID:  streamID,
		endpoint:  *ep.DataEndpoint + "/putMedia",
		signer:    c.signer,
		cliConfig: c.cliConfig,
	}, nil
}

func (p *Provider) PutMedia(ch chan *Cluster) (io.ReadCloser, error) {
	data :=
		struct {
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
				Cluster: ch,
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

	r, w := io.Pipe()
	chErr := make(chan error)
	go func() {
		if err := ebml.Marshal(&data, w); err != nil {
			chErr <- err
		}
		close(chErr)
		w.CloseWithError(io.EOF)
	}()

	req, err := http.NewRequest("POST", p.endpoint, r)
	if err != nil {
		return nil, err
	}
	if p.streamID.StreamName() != nil {
		req.Header.Set("x-amzn-stream-name", *p.streamID.StreamName())
	}
	if p.streamID.StreamARN() != nil {
		req.Header.Set("x-amzn-stream-arn", *p.streamID.StreamARN())
	}
	req.Header.Set("x-amzn-fragment-timecode-type", "RELATIVE")
	req.Header.Set("x-amzn-producer-start-timestamp", "0")

	_, err = p.signer.Presign(
		req, bytes.NewReader([]byte{}),
		p.cliConfig.SigningName, p.cliConfig.SigningRegion,
		10*time.Minute, time.Now(),
	)
	if err != nil {
		return nil, err
	}
	res, err := p.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	err, ok := <-chErr
	if !ok && err != nil {
		return nil, err
	}
	return res.Body, nil
}
