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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"

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
	streamID  StreamID
	endpoint  string
	signer    *v4.Signer
	cliConfig *client.Config
	tracks    []TrackEntry

	bufferPool sync.Pool
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
	httpClient             http.Client
	tags                   func() []SimpleTag
	onError                func(error)
	retryCount             int
	retryIntervalBase      time.Duration
	fragmentHeadDumpLen    int
	lenBlockBuffer         int
	lenResponseBuffer      int
	logger                 LoggerIF
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
		httpClient: http.Client{
			Timeout: 15 * time.Second,
		},
		lenBlockBuffer:    10,
		lenResponseBuffer: 10,
		logger:            Logger(),
	}
	for _, o := range opts {
		o(options)
	}

	var conn, nextConn *connection
	var lastAbsTime uint64
	chConnection := make(chan *connection)
	cleanConnections := func() {
		if conn != nil {
			conn.close()
			conn = nil
		}
		if nextConn != nil {
			nextConn.close()
			nextConn = nil
		}
		lastAbsTime = 0
	}
	var timeout *time.Timer
	resetTimeout := func() {
		timeout = time.AfterFunc(options.connectionTimeout, func() {
			options.logger.Warnf(`Receiving block timed out, clean connections: { StreamID: "%s" }`, p.streamID)
			cleanConnections()
		})
	}
	resetTimeout()

	chResp := make(chan *FragmentEvent, options.lenResponseBuffer)
	ctx, cancel := context.WithCancel(context.Background())

	allDone := make(chan struct{})
	go func() {
		p.putSegments(ctx, chConnection, chResp, options)
		close(allDone)
	}()

	shutdown := func(ctx context.Context) error {
		timeout.Stop()
		cleanConnections()
		if chConnection != nil {
			close(chConnection)
			chConnection = nil
		}
		select {
		case <-allDone:
			cancel()
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	prepareNextConn := func() {
		nextConn = newConnection(options)
		chConnection <- nextConn
	}
	switchToNextConn := func(startTime uint64) {
		if conn != nil {
			conn.close()
		}
		timeout.Stop()
		conn = nextConn
		conn.initialize(startTime, options)
		resetTimeout()
		nextConn = nil
	}

	writer := &blockWriter{
		fnWrite: func(bt *BlockWithBaseTimecode) error {
			absTime := uint64(bt.AbsTimecode())
			if lastAbsTime != 0 {
				diff := int64(absTime - lastAbsTime)
				if diff < 0 {
					return fmt.Errorf(`stream_id=%s, timecode=%d, last=%d, diff=%d: %w`,
						p.streamID, bt.AbsTimecode(), lastAbsTime, diff,
						ErrInvalidTimecode,
					)
				}
				if diff > math.MaxInt16 {
					options.logger.Debugf(`Forcing next connection: { StreamID: "%s", AbsTime: %d, LastAbsTime: %d, Diff: %d }`,
						p.streamID, bt.AbsTimecode(), lastAbsTime, diff,
					)
					if nextConn == nil {
						prepareNextConn()
					}
					switchToNextConn(absTime)
				}
			}

			if conn == nil || (nextConn == nil && int16(absTime-conn.baseTimecode) > 8000) {
				options.logger.Debugf(`Prepare next connection: { StreamID: "%s" }`, p.streamID)
				prepareNextConn()
			}
			if conn == nil || int16(absTime-conn.baseTimecode) > 9000 {
				options.logger.Debugf(`Switch to next connection: { StreamID: "%s", AbsTime: %d }`, p.streamID, absTime)
				switchToNextConn(absTime)
			}
			bt.Block.Timecode = int16(absTime - conn.baseTimecode)
			select {
			case conn.Block <- bt.Block:
				conn.countBlock()
				lastAbsTime = absTime
			case <-timeout.C:
				cleanConnections()
				return fmt.Errorf(`stream_id=%s, timecode=%d: %w`,
					p.streamID, bt.AbsTimecode(),
					ErrWriteTimeout,
				)
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
			return shutdown(ctx)
		},
		fnClose: func() error {
			cancel()
			return shutdown(context.Background())
		},
	}

	return writer, nil
}

func (p *Provider) putSegments(ctx context.Context, ch chan *connection, chResp chan *FragmentEvent, opts *PutMediaOptions) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(chResp)
	}()

	for conn := range ch {
		conn := conn
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts.logger.Debugf("New conn: %d", conn.baseTimecode)
			err := p.putMedia(ctx, conn, chResp, opts)
			opts.logger.Debugf("Finished conn: %d", conn.baseTimecode)
			if err != nil {
				opts.onError(err)
				return
			}
		}()
	}
}

func (p *Provider) putMedia(ctx context.Context, conn *connection, chResp chan *FragmentEvent, opts *PutMediaOptions) error {
	segmentUuid := opts.segmentUID
	if segmentUuid == nil {
		var err error
		segmentUuid, err = generateRandomUUID()
		if err != nil {
			return err
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
				Timecode:    conn.BlockChWithBaseTimecode.Timecode,
				SimpleBlock: conn.BlockChWithBaseTimecode.Block,
			},
			Tags: Tags{
				Tag: conn.BlockChWithBaseTimecode.Tag,
			},
		},
	}

	r, wOutRaw := io.Pipe()
	wOutBuf := bufio.NewWriter(wOutRaw)
	writeErr := func() error { return nil }
	var w io.Writer
	var backup *bytes.Buffer

	if opts.retryCount > 0 {
		// Ignore error when http request body is closed.
		// Continue marshalling whole fragment and retry sending later.
		noErrWriter := &ignoreErrWriter{Writer: wOutBuf}
		writeErr = noErrWriter.Err

		// Take copy of the fragment.
		backup = p.bufferPool.Get().(*bytes.Buffer)
		defer p.bufferPool.Put(backup)
		backup.Reset()
		w = io.MultiWriter(backup, noErrWriter)
	} else {
		w = io.Writer(wOutBuf)
	}

	var errFlush, errMarshal error
	chMarshalDone := make(chan struct{})
	go func() {
		defer func() {
			opts.logger.Debug("Finished EBML marshalling")
			close(chMarshalDone)
			wOutRaw.CloseWithError(io.EOF)
		}()
		if err := ebml.Marshal(&data, w); err != nil {
			errMarshal = fmt.Errorf("ebml marshalling: %w", err)
			return
		}
		if err := wOutBuf.Flush(); err != nil {
			errFlush = fmt.Errorf("flushing buffer: %w", err)
		}
	}()

	chRespRaw := make(chan *FragmentEvent)

	var wgResp sync.WaitGroup
	defer func() {
		opts.logger.Debug("Flushing responses")
		close(chRespRaw)
		wgResp.Wait()
	}()
	wgResp.Add(1)
	go func() {
		defer wgResp.Done()
		for fe := range chRespRaw {
			if fe.ErrorId == INVALID_MKV_DATA && conn.numBlock() == 0 {
				// Ignore INVALID_MKV_DATA due to zero Block segment.
				continue
			}
			chResp <- fe
		}
	}()

	errPutMedia := p.putMediaRaw(ctx, r, chRespRaw, opts)
	_ = r.Close()

	<-chMarshalDone
	if errMarshal != nil {
		// Marshal error is not recoverable.
		return errMarshal
	}
	if conn.numBlock() == 0 {
		// No Block is written and INVALID_MKV_DATA is returned.
		return nil
	}

	err := newMultiError(errPutMedia, errFlush, writeErr())
	if err != nil && opts.retryCount > 0 {
		opts.logger.Debug("Retrying PutMedia")
		interval := opts.retryIntervalBase
	L_RETRY:
		for i := 0; i < opts.retryCount; i++ {
			select {
			case <-time.After(interval):
			case <-ctx.Done():
				break L_RETRY
			}

			opts.logger.Infof(
				`Retrying PutMedia: { StreamID: "%s", RetryCount: %d, Err: %s }`,
				p.streamID, i,
				string(regexAmzCredHeader.ReplaceAll([]byte(strconv.Quote(err.Error())), []byte("X-Amz-$1=***"))),
			)
			if err = p.putMediaRaw(ctx, bytes.NewReader(backup.Bytes()), chRespRaw, opts); err == nil {
				break
			}
			if fe, ok := err.(*FragmentEventError); ok && opts.fragmentHeadDumpLen > 0 {
				bb := backup.Bytes()
				if len(bb) > opts.fragmentHeadDumpLen {
					fe.fragmentHead = bb[:opts.fragmentHeadDumpLen]
				} else {
					fe.fragmentHead = bb
				}
			}
			interval *= 2
		}
	}
	return err
}

func (p *Provider) putMediaRaw(ctx context.Context, r io.Reader, chResp chan *FragmentEvent, opts *PutMediaOptions) error {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, "POST", p.endpoint, r)
	if err != nil {
		return fmt.Errorf("creating http request: %w", err)
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
		return fmt.Errorf("presigning request: %w", err)
	}
	res, err := opts.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending http request: %w", err)
	}

	defer func() {
		_ = res.Body.Close()
		opts.logger.Debug("API connection closed")
	}()

	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("reading http response: %w", err)
		}
		return fmt.Errorf("%d: %s", res.StatusCode, string(body))
	}
	chFE := make(chan *FragmentEvent)
	chErr := make(chan error, 1)
	go func() {
		for fe := range chFE {
			switch fe.EventType {
			case FRAGMENT_EVENT_ERROR:
				chErr <- fe.AsError()
				cancel()
			case FRAGMENT_EVENT_PERSISTED:
				cancel()
			}
			chResp <- fe
		}
		close(chErr)
	}()
	if err := parseFragmentEvent(
		res.Body, chFE,
	); err != nil && err != context.Canceled {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return <-chErr
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
