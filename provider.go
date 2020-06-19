package kinesisvideomanager

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"

	"github.com/at-wat/ebml-go"

	"github.com/google/uuid"
)

const TimecodeScale = 1000000

var immediateTimeout chan time.Time

func init() {
	immediateTimeout = make(chan time.Time)
	close(immediateTimeout)
}

type Provider struct {
	streamID  StreamID
	endpoint  string
	signer    *v4.Signer
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
	httpClient             http.Client
	tags                   func() []SimpleTag
	onError                func(error)
}

type PutMediaOption func(*PutMediaOptions)

type connection struct {
	*BlockChWithBaseTimecode
	baseTimecode uint64
	onceClose    sync.Once
	onceInit     sync.Once
	timeout      <-chan time.Time
}

func newConnection() *connection {
	return &connection{
		BlockChWithBaseTimecode: &BlockChWithBaseTimecode{
			Timecode: make(chan uint64, 1),
			Block:    make(chan ebml.Block),
			Tag:      make(chan *Tag, 1),
		},
		timeout: immediateTimeout,
	}
}
func (c *connection) initialize(baseTimecode uint64, opts *PutMediaOptions) {
	c.onceInit.Do(func() {
		c.baseTimecode = baseTimecode
		c.Timecode <- c.baseTimecode
		close(c.Timecode)

		if opts.tags != nil {
			c.Tag <- &Tag{SimpleTag: opts.tags()}
		}
		close(c.Tag)

		c.timeout = time.After(opts.connectionTimeout)
	})
}

func (c *connection) close() {
	// Ensure Timecode and Tag channels are closed
	c.initialize(0, &PutMediaOptions{})

	c.onceClose.Do(func() {
		close(c.Block)
	})
}

func (p *Provider) PutMedia(ch chan *BlockWithBaseTimecode, chResp chan FragmentEvent, opts ...PutMediaOption) {
	options := &PutMediaOptions{
		title:                  "kinesisvideomanager.Provider",
		fragmentTimecodeType:   FragmentTimecodeTypeRelative,
		producerStartTimestamp: "0",
		connectionTimeout:      15 * time.Second,
		onError:                func(err error) { Logger().Error(err) },
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

		lastAbsTime := uint64(0)
		cleanConnections := func() {
			conn.close()
			conn = nil
			if nextConn != nil {
				nextConn.close()
				nextConn = nil
			}
			lastAbsTime = 0
		}
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
				if lastAbsTime != 0 {
					diff := int64(absTime - lastAbsTime)
					if diff < 0 || diff > math.MaxInt16 {
						Logger().Warnf(
							"Invalid timecode (streamID:%s timecode:%d last:%d diff:%d)",
							p.streamID, bt.AbsTimecode(), lastAbsTime, diff,
						)
						continue
					}
				}

				if conn == nil || (nextConn == nil && int16(absTime-conn.baseTimecode) > 8000) {
					Logger().Debugf("Prepare next connection (streamID:%s)", p.streamID)
					nextConn = newConnection()
					chBlockChWithBaseTimecode <- nextConn.BlockChWithBaseTimecode
				}
				if conn == nil || int16(absTime-conn.baseTimecode) > 9000 {
					Logger().Debugf("Switch to next connection (streamID:%s absTime:%d)", p.streamID, absTime)
					if conn != nil {
						conn.close()
					}
					conn = nextConn
					conn.initialize(absTime, options)
					nextConn = nil
				}
				bt.Block.Timecode = int16(absTime - conn.baseTimecode)
				if conn != nil {
					timeout = conn.timeout
				}
				select {
				case conn.Block <- bt.Block:
					lastAbsTime = absTime
				case <-timeout:
					Logger().Warnf("Sending block timed out, clean connections (streamID:%s)", p.streamID)
					cleanConnections()
				}
			case <-timeout:
				Logger().Warnf("Receiving block timed out, clean connections (streamID:%s)", p.streamID)
				cleanConnections()
			}
		}
	}()

	p.putSegments(chBlockChWithBaseTimecode, chResp, options)
}

func (p *Provider) putSegments(ch chan *BlockChWithBaseTimecode, chResp chan FragmentEvent, opts *PutMediaOptions) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(chResp)
	}()

	for seg := range ch {
		seg := seg
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
				opts.onError(err)
				return
			}

			var fes []FragmentEvent
			fes, err = parseFragmentEvent(res)
			if err != nil {
				opts.onError(err)
				return
			}
			for _, fe := range fes {
				chResp <- fe
			}
		}()
	}
}

func (p *Provider) putMedia(baseTimecode chan uint64, ch chan ebml.Block, chTag chan *Tag, opts *PutMediaOptions) (io.ReadCloser, error) {
	segmentUuid := opts.segmentUID
	if segmentUuid == nil {
		var err error
		segmentUuid, err = generateRandomUUID()
		if err != nil {
			return nil, err
		}
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
		defer func() {
			close(chErr)
			w.CloseWithError(io.EOF)
		}()

		buf := bufio.NewWriter(w)
		if err := ebml.Marshal(&data, buf); err != nil {
			chErr <- err
			return
		}
		if err := buf.Flush(); err != nil {
			chErr <- err
			return
		}
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
	res, err := opts.httpClient.Do(req)
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
	if err := <-chErr; err != nil {
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

func WithHttpClient(client http.Client) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.httpClient = client
	}
}

func WithTags(tags func() []SimpleTag) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.tags = tags
	}
}

func OnError(onError func(error)) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.onError = onError
	}
}
