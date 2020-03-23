package kinesisvideomanager

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"

	"github.com/at-wat/ebml-go"

	"github.com/google/uuid"
)

const TimecodeScale = 1000000

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

type PutMediaOptions struct {
	segmentUID             []byte
	title                  string
	fragmentTimecodeType   FragmentTimecodeType
	producerStartTimestamp string
	connectionTimeout      time.Duration
	tags                   func() []SimpleTag
}

type PutMediaOption func(*PutMediaOptions)

type connection struct {
	*BlockChWithBaseTimecode
	baseTimecode uint64
	once         sync.Once
	timeout      <-chan time.Time
}

func (c *connection) initialize(firstBlock *BlockWithBaseTimecode, opts *PutMediaOptions) {
	c.baseTimecode = uint64(firstBlock.AbsTimecode())
	c.Timecode <- c.baseTimecode
	close(c.Timecode)

	if opts.tags != nil {
		c.Tag <- &Tag{SimpleTag: opts.tags()}
	}
	close(c.Tag)

	c.timeout = time.After(opts.connectionTimeout)
}

func (c *connection) close() {
	c.once.Do(func() {
		close(c.Block)
	})
}

func (p *Provider) PutMedia(ch chan *BlockWithBaseTimecode, chResp chan FragmentEvent, opts ...PutMediaOption) error {
	segmentUuid, err := generateRandomUUID()
	if err != nil {
		return err
	}
	options := &PutMediaOptions{
		segmentUID:             segmentUuid,
		title:                  "kinesisvideomanager.Provider",
		fragmentTimecodeType:   FragmentTimecodeTypeRelative,
		producerStartTimestamp: "0",
		connectionTimeout:      15 * time.Second,
	}
	for _, o := range opts {
		o(options)
	}

	chBlockChWithBaseTimecode := make(chan *BlockChWithBaseTimecode)
	go func() {
		var conn, nextConn *connection
		defer func() {
			if conn != nil {
				conn.close()
			}
			if nextConn != nil {
				nextConn.close()
			}
			close(chBlockChWithBaseTimecode)
		}()

		var lastBlock *BlockWithBaseTimecode
		for {
			var timeout <-chan time.Time
			if conn != nil {
				timeout = conn.timeout
			}
			select {
			case bt, ok := <-ch:
				if !ok {
					return
				}
				absTime := uint64(bt.AbsTimecode())
				if conn == nil || (nextConn == nil && conn.baseTimecode+8000 < absTime) {
					// Prepare next connection
					nextConn = &connection{
						BlockChWithBaseTimecode: &BlockChWithBaseTimecode{
							Timecode: make(chan uint64, 1),
							Block:    make(chan ebml.Block),
							Tag:      make(chan *Tag, 1),
						},
					}
					chBlockChWithBaseTimecode <- nextConn.BlockChWithBaseTimecode
				}
				if conn == nil || conn.baseTimecode+9000 < absTime {
					// Switch to next connection
					if conn != nil {
						conn.close()
					}
					conn = nextConn
					conn.initialize(bt, options)
					nextConn = nil
				}
				bt.Block.Timecode = int16(absTime - conn.baseTimecode)
				conn.Block <- bt.Block
				lastBlock = bt
			case <-timeout:
				// Forcefully switch to next connection
				conn.close()
				conn = nextConn
				if conn != nil {
					conn.initialize(lastBlock, options)
				}
				nextConn = nil
			}
		}
	}()

	return p.putSegments(chBlockChWithBaseTimecode, chResp, options)
}

func (p *Provider) putSegments(ch chan *BlockChWithBaseTimecode, chResp chan FragmentEvent, opts *PutMediaOptions) error {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
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
		wg.Add(1)
		go func() {
			defer func() {
				wg.Done()
			}()
			res, err := p.putMedia(seg.Timecode, seg.Block, seg.Tag, opts)
			if res != nil {
				defer res.Close()
			}
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

func (p *Provider) putMedia(baseTimecode chan uint64, ch chan ebml.Block, chTag chan *Tag, opts *PutMediaOptions) (io.ReadCloser, error) {
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
				SegmentUID:    opts.segmentUID,
				TimecodeScale: TimecodeScale,
				Title:         opts.title,
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
	req.Header.Set("x-amzn-fragment-timecode-type", string(opts.fragmentTimecodeType))
	req.Header.Set("x-amzn-producer-start-timestamp", opts.producerStartTimestamp)

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
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%d: %s", res.StatusCode, string(body))
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

func WithSegmentUID(segmentUID []byte) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.segmentUID = segmentUID
	}
}

func WithTitle(title string) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.title = title
	}
}

func WithFragmentTimecodeType(fragmentTimecodeType FragmentTimecodeType) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.fragmentTimecodeType = fragmentTimecodeType
	}
}

func WithProducerStartTimestamp(producerStartTimestamp time.Time) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.producerStartTimestamp = ToTimestamp(producerStartTimestamp)
	}
}

func WithConnectionTimeout(timeout time.Duration) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.connectionTimeout = timeout
	}
}

func WithTags(tags func() []SimpleTag) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.tags = tags
	}
}
