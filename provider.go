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

	"github.com/google/uuid"
)

type Provider struct {
	streamID  StreamID
	endpoint  string
	signer    *v4.Signer
	httpCli   http.Client
	cliConfig *client.Config
	tracks    []TrackEntry
}

func (c *Client) Provider(streamID StreamID, tracks []TrackEntry) (*Provider, error) {
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
		tracks:    tracks,
	}, nil
}

func (p *Provider) PutMedia(ch chan *BlockWithBaseTimecode, chTag chan *Tag, chResp chan FragmentEvent) error {
	chBlockChWithBaseTimecode := make(chan *BlockChWithBaseTimecode)
	go func() {
		defer close(chBlockChWithBaseTimecode)

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

	return p.putSegments(chBlockChWithBaseTimecode, chResp)
}

func (p *Provider) putSegments(ch chan *BlockChWithBaseTimecode, chResp chan FragmentEvent) error {
	defer func() {
		close(chResp)
	}()
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

			var fes []FragmentEvent
			fes, err = parseFragmentEvent(res)
			if err != nil {
				chErr <- err
				return
			}
			for _, fe := range fes {
				chResp <- fe
			}
		}()
	}
}

func (p *Provider) putMedia(baseTimecode uint64, ch chan ebml.Block, chTag chan *Tag) (io.ReadCloser, error) {
	segmentUuid, err := generateRandomUUID()
	if err != nil {
		return nil, err
	}

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
				SegmentUID:    segmentUuid,
				TimecodeScale: 1000000,
				Title:         "kinesisvideomanager.Provider",
				MuxingApp:     "kinesisvideomanager.Provider",
				WritingApp:    "kinesisvideomanager.Provider",
			},
			Tracks: Tracks{
				TrackEntry: p.tracks,
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

func generateRandomUUID() ([]byte, error) {
	return uuid.New().MarshalBinary()
}
