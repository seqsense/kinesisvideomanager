// Copyright 2020 SEQSENSE, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kinesisvideomanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"

	"github.com/at-wat/ebml-go"

	"github.com/google/uuid"
)

const TimecodeScale = 1000000

var (
	immediateTimeout chan time.Time

	regexAmzCredHeader = regexp.MustCompile(`X-Amz-(Credential|Security-Token|Signature)=[^&]*`)
)

var (
	ErrInvalidTimecode = errors.New("invalid timecode")
	ErrWriteTimeout    = errors.New("write timeout")
)

func init() {
	immediateTimeout = make(chan time.Time)
	close(immediateTimeout)
}

type Provider struct {
	streamID StreamID
	endpoint string
	tracks   []TrackEntry
	cli      *Client

	bufferPool sync.Pool
}

// Provider creates a KVS provider client.
// Passed context is used only to get the KVS data endpint
// and does not control the future data write session.
func (c *Client) Provider(ctx context.Context, streamID StreamID, tracks []TrackEntry) (*Provider, error) {
	ep, err := c.kv.GetDataEndpoint(
		ctx,
		&kinesisvideo.GetDataEndpointInput{
			APIName:    types.APINamePutMedia,
			StreamName: streamID.StreamName(),
			StreamARN:  streamID.StreamARN(),
		},
	)
	if err != nil {
		return nil, err
	}
	return &Provider{
		streamID: streamID,
		endpoint: *ep.DataEndpoint + "/putMedia",
		tracks:   tracks,
		cli:      c,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 1024))
			},
		},
	}, nil
}

type BlockWriter interface {
	// Write a block to Kinesis Video Stream.
	Write(*BlockWithBaseTimecode) error
	// ReadResponse reads a response from Kinesis Video Stream.
	ReadResponse() (*FragmentEvent, error)
	// Close immediately shuts down the client.
	Close() error
	// Shutdown gracefully shuts down the client without interrupting on-going PutMedia request.
	// If Shotdown returned an error, some of the internal resources might not released yet and
	// caller should call Shutdown or Close again.
	Shutdown(ctx context.Context) error
}

type blockWriter struct {
	fnWrite        func(*BlockWithBaseTimecode) error
	fnReadResponse func() (*FragmentEvent, error)
	fnClose        func() error
	fnShutdown     func(ctx context.Context) error
}

func (w *blockWriter) Write(bt *BlockWithBaseTimecode) error {
	return w.fnWrite(bt)
}

func (w *blockWriter) ReadResponse() (*FragmentEvent, error) {
	return w.fnReadResponse()
}

func (w *blockWriter) Close() error {
	return w.fnClose()
}

func (w *blockWriter) Shutdown(ctx context.Context) error {
	return w.fnShutdown(ctx)
}

type PutMediaOptions struct {
	segmentUID             []byte
	title                  string
	fragmentTimecodeType   FragmentTimecodeType
	producerStartTimestamp string
	connectionTimeout      time.Duration
	httpClient             aws.HTTPClient
	tags                   func() []SimpleTag
	retryCount             int
	retryIntervalBase      time.Duration
	fragmentHeadDumpLen    int
	lenBlockBuffer         int
	lenResponseBuffer      int
	logger                 LoggerIF

	onError      func(error)
	onNewConn    func()
	onSwitchConn func(uint64)
}

type PutMediaOption func(*PutMediaOptions)

type connection struct {
	*BlockChWithBaseTimecode
	baseTimecode uint64
	onceClose    sync.Once
	onceInit     sync.Once
	nBlock       uint64
}

func newConnection(opts *PutMediaOptions) *connection {
	return &connection{
		BlockChWithBaseTimecode: &BlockChWithBaseTimecode{
			Timecode: make(chan uint64, 1),
			Block:    make(chan ebml.Block, opts.lenBlockBuffer),
			Tag:      make(chan *Tag, 1),
		},
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
	})
}

func (c *connection) close() {
	// Ensure Timecode and Tag channels are closed
	c.initialize(0, &PutMediaOptions{})

	c.onceClose.Do(func() {
		close(c.Block)
	})
}

func (c *connection) countBlock() {
	atomic.AddUint64(&c.nBlock, 1)
}

func (c *connection) numBlock() int {
	return int(atomic.LoadUint64(&c.nBlock))
}

// PutMedia opens connection to Kinesis Video Stream to put media blocks.
// This function immediately returns BlockWriter.
// BlockWriter.ReadResponse() must be called until getting io.EOF as error,
// otherwise Write() call will be blocked after the buffer is filled.
func (p *Provider) PutMedia(opts ...PutMediaOption) (BlockWriter, error) {
	var options *PutMediaOptions
	options = &PutMediaOptions{
		title:                  "kinesisvideomanager.Provider",
		fragmentTimecodeType:   FragmentTimecodeTypeRelative,
		producerStartTimestamp: "0",
		connectionTimeout:      15 * time.Second,
		onError:                func(err error) { options.logger.Error(err) },
		httpClient: &http.Client{
			Timeout: 0,
		},
		lenBlockBuffer:    10,
		lenResponseBuffer: 10,
		logger:            Logger(),
	}
	for _, o := range opts {
		o(options)
	}
	segmentUuid := options.segmentUID
	if segmentUuid == nil {
		var err error
		segmentUuid, err = generateRandomUUID()
		if err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	r, w := io.Pipe()

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, r)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating http request: %w", err)
	}
	if p.streamID.StreamName() != nil {
		req.Header.Set("x-amzn-stream-name", *p.streamID.StreamName())
	}
	if p.streamID.StreamARN() != nil {
		req.Header.Set("x-amzn-stream-arn", *p.streamID.StreamARN())
	}
	req.Header.Set("x-amzn-fragment-timecode-type", string(options.fragmentTimecodeType))
	req.Header.Set("x-amzn-producer-start-timestamp", options.producerStartTimestamp)

	if err := p.cli.sign(ctx, req, nil); err != nil {
		cancel()
		return nil, fmt.Errorf("presigning http request: %w", err)
	}
	res, err := options.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("sending http request: %w", err)
	}

	header := struct {
		Header  EBMLHeader  `ebml:"EBML"`
		Segment SegmentHead `ebml:",size=unknown"`
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
		Segment: SegmentHead{
			Info: Info{
				SegmentUID:    segmentUuid,
				TimecodeScale: TimecodeScale,
				Title:         options.title,
				MuxingApp:     "kinesisvideomanager.Provider",
				WritingApp:    "kinesisvideomanager.Provider",
			},
			Tracks: Tracks{
				TrackEntry: p.tracks,
			},
		},
	}
	if err := ebml.Marshal(&header, w); err != nil {
		cancel()
		return nil, err
	}

	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("reading http response: %w", err)
		}
		return nil, fmt.Errorf("%d: %s", res.StatusCode, string(body))
	}

	chResp := make(chan *FragmentEvent, options.lenResponseBuffer)
	chFE := make(chan *FragmentEvent)
	go func() {
		for fe := range chFE {
			switch fe.EventType {
			case FRAGMENT_EVENT_ERROR:
				cancel()
			case FRAGMENT_EVENT_PERSISTED:
			}
			chResp <- fe
		}
		close(chResp)
	}()
	go func() {
		if err := parseFragmentEvent(
			res.Body, chFE,
		); err != nil && err != context.Canceled {
			println("parseFragmentEvent", err.Error())
		}
	}()

	const invalidTimecode = uint64(0xFFFFFFFFFFFFFFFF)
	var clusterTimecode uint64 = invalidTimecode

	writer := &blockWriter{
		fnWrite: func(bt *BlockWithBaseTimecode) error {
			absTime := uint64(bt.AbsTimecode())
			if clusterTimecode == invalidTimecode || absTime > clusterTimecode+9000 {
				clusterTimecode = absTime
				cluster := struct {
					Cluster ClusterHead `ebml:",size=unknown"`
				}{
					Cluster: ClusterHead{
						Timecode: clusterTimecode,
					},
				}
				if err := ebml.Marshal(&cluster, w); err != nil {
					return err
				}
			}
			bt.Block.Timecode = int16(absTime - clusterTimecode)
			block := struct {
				SimpleBlock *ebml.Block
			}{
				SimpleBlock: &bt.Block,
			}
			if err := ebml.Marshal(&block, w); err != nil {
				return err
			}
			return nil
		},
		fnReadResponse: func() (*FragmentEvent, error) {
			resp, ok := <-chResp
			if !ok {
				return nil, io.EOF
			}
			return resp, nil
		},
		fnShutdown: func(ctx context.Context) error {
			cancel()
			return nil
		},
		fnClose: func() error {
			_ = res.Body.Close()
			cancel()
			return nil
		},
	}

	return writer, nil
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

// WithFragmentHeadDumpLen sets fragment data head dump length embedded to the FragmentEvent error message.
// Data dump is enabled only if PutMediaRetry is enabled.
// Set zero to disable.
func WithFragmentHeadDumpLen(n int) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.fragmentHeadDumpLen = n
	}
}

func WithHttpClient(client aws.HTTPClient) PutMediaOption {
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

// OnPutMediaNewConn registers a func that will be called before
// creating a new PutMedia API connection.
// Media stream processing is blocked until the func returns.
func OnPutMediaNewConn(onNewConn func()) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.onNewConn = onNewConn
	}
}

// OnPutMediaSwitchConn registers a func that will be called before
// switching a PutMedia API connection.
// Media stream processing is blocked until the func returns.
func OnPutMediaSwitchConn(onSwitchConn func(timecode uint64)) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.onSwitchConn = onSwitchConn
	}
}

func WithPutMediaRetry(count int, intervalBase time.Duration) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.retryCount = count
		p.retryIntervalBase = intervalBase
	}
}

func WithPutMediaBufferLen(n int) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.lenBlockBuffer = n
	}
}

func WithPutMediaResponseBufferLen(n int) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.lenResponseBuffer = n
	}
}

func WithPutMediaLogger(logger LoggerIF) PutMediaOption {
	return func(p *PutMediaOptions) {
		p.logger = logger
	}
}
