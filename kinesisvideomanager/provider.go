package kinesisvideomanager

import (
	"bytes"
	"io"
	"log"
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

func (p *Provider) PutMedia(ch chan *BlockWithBaseTimecode, chTag chan *Tag) error {
	chBlockChWithBaseTimecode := make(chan *BlockChWithBaseTimecode)
	go func() {
		var nextConn *BlockChWithBaseTimecode
		var conn *BlockChWithBaseTimecode
		for {
			select {
			case tag := <-chTag:
				conn.Tag <- tag
			case bt, ok := <-ch:
				if !ok {
					return
				}
				absTime := uint64(int64(bt.Timecode) + int64(bt.Block.Timecode))
				if conn == nil || (nextConn == nil && conn.Timecode+8000 < absTime) {
					// Prepare next connection
					chBlock := make(chan ebml.Block)
					chTag := make(chan *Tag)
					nextConn = &BlockChWithBaseTimecode{
						Timecode: absTime + 1000,
						Block:    chBlock,
						Tag:      chTag,
					}
					chBlockChWithBaseTimecode <- nextConn
				}
				if conn == nil || conn.Timecode+9000 < absTime {
					// Switch to next connection
					if conn != nil {
						close(conn.Block)
						close(conn.Tag)
					}
					conn = nextConn
					nextConn = nil
				}
				bt.Block.Timecode = int16(absTime - conn.Timecode)
				conn.Block <- bt.Block
			}
		}
	}()

	return p.putSegments(chBlockChWithBaseTimecode)
}

func (p *Provider) putSegments(ch chan *BlockChWithBaseTimecode) error {
	chErr := make(chan error)
	for {
		var seg *BlockChWithBaseTimecode
		var ok bool
		select {
		case seg, ok = <-ch:
			if !ok {
				return io.EOF
			}
		case err := <-chErr:
			return err
		}
		go func() {
			res, err := p.putMedia(seg.Timecode, seg.Block, seg.Tag)
			if err != nil {
				chErr <- err
				return
			}
			for {
				b := make([]byte, 4096)
				n, err := res.Read(b)
				if err == io.EOF {
					res.Close()
					return
				}
				if err != nil {
					chErr <- err
					return
				}
				// TODO: parse and return
				log.Printf("response: %s", string(b[:n]))
			}
		}()
	}
}

func (p *Provider) putMedia(baseTimecode uint64, ch chan ebml.Block, chTag chan *Tag) (io.ReadCloser, error) {
	data := struct {
		Header  EBMLHeader   `ebml:"EBML"`
		Segment SegmentWrite `ebml:",size=unknown"`
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
		Segment: SegmentWrite{
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
			Cluster: ClusterWrite{
				Timecode:    baseTimecode,
				SimpleBlock: ch,
			},
			Tags: Tags{
				Tag: chTag,
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
