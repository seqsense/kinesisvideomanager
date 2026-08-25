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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	}, nil
}

type BlockWriter interface {
	// Write a block to Kinesis Video Stream.
	Write(*BlockWithBaseTimecode) error
	// ReadResponse reads a response from Kinesis Video Stream.
	ReadResponse() (*FragmentEvent, error)
	// Close shuts down the client.
	Close() error
}

type blockWriter struct {
	fnWrite        func(*BlockWithBaseTimecode) error
	fnReadResponse func() (*FragmentEvent, error)
	fnClose        func() error
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

type PutMediaOptions struct {
	segmentUID             []byte
	title                  string
	fragmentTimecodeType   FragmentTimecodeType
	producerStartTimestamp string
	httpClient             aws.HTTPClient
	tags                   func() []SimpleTag
}

type PutMediaOption func(*PutMediaOptions)

// PutMedia opens connection to Kinesis Video Stream to put media blocks.
// This function immediately returns BlockWriter.
func (p *Provider) PutMedia(opts ...PutMediaOption) (BlockWriter, error) {
	var options *PutMediaOptions
	options = &PutMediaOptions{
		title:                  "kinesisvideomanager.Provider",
		fragmentTimecodeType:   FragmentTimecodeTypeRelative,
		producerStartTimestamp: "0",
		httpClient:             http.DefaultClient,
	}
	for _, o := range opts {
		o(options)
	}

	chResp := make(chan *FragmentEvent)

	newConnection := func(ctx context.Context) (io.Writer, func() error, error) {
		r, w := io.Pipe()

		req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, r)
		if err != nil {
			return nil, nil, fmt.Errorf("creating http request: %w", err)
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
			return nil, nil, fmt.Errorf("presigning http request: %w", err)
		}
		res, err := options.httpClient.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("sending http request: %w", err)
		}

		if res.StatusCode != 200 {
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("reading http response: %w", err)
			}
			return nil, nil, fmt.Errorf("%d: %s", res.StatusCode, string(body))
		}

		segmentUuid := options.segmentUID
		if segmentUuid == nil {
			var err error
			segmentUuid, err = generateRandomUUID()
			if err != nil {
				return nil, nil, err
			}
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
			return nil, nil, err
		}

		decResp := json.NewDecoder(res.Body)
		go func() {
			for {
				var fe FragmentEvent
				if err := decResp.Decode(&fe); err != nil {
					println(err.Error()) // TODO
					close(chResp)
					return
				}
				chResp <- &fe
			}
		}()

		return w, func() error {
			return res.Body.Close()
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	w, fnClose, err := newConnection(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	const invalidTimecode = uint64(0xFFFFFFFFFFFFFFFF)
	var clusterTimecode uint64 = invalidTimecode

	writeTags := func() error {
		if options.tags == nil {
			return nil
		}
		tags := options.tags()
		if len(tags) == 0 {
			return nil
		}
		data := struct {
			Tags TagsWrite
		}{
			Tags: TagsWrite{
				Tag: Tag{
					SimpleTag: tags,
				},
			},
		}
		return ebml.Marshal(&data, w)
	}

	writer := &blockWriter{
		fnWrite: func(bt *BlockWithBaseTimecode) error {
			absTime := uint64(bt.AbsTimecode())
			if clusterTimecode == invalidTimecode || absTime > clusterTimecode+9000 {
				if clusterTimecode != invalidTimecode {
					if err := writeTags(); err != nil {
						return err
					}
				}
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
			fe, ok := <-chResp
			if !ok {
				return nil, io.EOF
			}
			return fe, nil
		},
		fnClose: func() error {
			_ = writeTags()
			return fnClose()
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
