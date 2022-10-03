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
	"encoding/json"
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
)

type Consumer struct {
	streamID  StreamID
	endpoint  string
	signer    *v4.Signer
	httpCli   http.Client
	cliConfig *client.Config
}

func (c *Client) Consumer(streamID StreamID) (*Consumer, error) {
	ep, err := c.kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String("GET_MEDIA"),
			StreamName: streamID.StreamName(),
			StreamARN:  streamID.StreamARN(),
		},
	)
	if err != nil {
		return nil, err
	}
	return &Consumer{
		streamID:  streamID,
		endpoint:  *ep.DataEndpoint + "/getMedia",
		signer:    c.signer,
		cliConfig: c.cliConfig,
	}, nil
}

type BlockReader interface {
	// Read returns media block.
	Read() (*BlockWithBaseTimecode, error)
	// ReadTag returns stored tag.
	ReadTag() (*Tag, error)
	// Close the connenction to Kinesis Video Stream.
	Close() (*Container, error)
}

type blockReader struct {
	fnRead    func() (*BlockWithBaseTimecode, error)
	fnReadTag func() (*Tag, error)
	fnClose   func() (*Container, error)
}

func (r *blockReader) Read() (*BlockWithBaseTimecode, error) {
	return r.fnRead()
}
func (r *blockReader) ReadTag() (*Tag, error) {
	return r.fnReadTag()
}
func (r *blockReader) Close() (*Container, error) {
	return r.fnClose()
}

// GetMedia opens connection to Kinesis Video Stream to get media blocks.
// This function immediately returns BlockReader.
// Both BlockWriter.Read() and BlockWriter.ReadTag() must be called until getting
// io.EOF as error, otherwise Reader will be blocked after the buffer is filled.
func (c *Consumer) GetMedia(opts ...GetMediaOption) (BlockReader, error) {
	options := &GetMediaOptions{
		startSelector: StartSelector{
			StartSelectorType: StartSelectorTypeNow,
		},
	}
	for _, o := range opts {
		o(options)
	}

	body, err := json.Marshal(
		&GetMediaBody{
			StartSelector: options.startSelector,
			StreamName:    c.streamID.StreamName(),
			StreamARN:     c.streamID.StreamARN(),
		})
	if err != nil {
		return nil, err
	}
	bodyReader := bytes.NewReader(body)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bodyReader)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-type", "application/json")

	_, err = c.signer.Presign(
		req, bodyReader,
		c.cliConfig.SigningName, c.cliConfig.SigningRegion,
		10*time.Minute, time.Now(),
	)
	if err != nil {
		cancel()
		return nil, err
	}
	res, err := c.httpCli.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		cancel()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%d: %s", res.StatusCode, string(body))
	}

	ch := make(chan *BlockWithBaseTimecode, options.lenBlockBuffer)
	chTag := make(chan *Tag, options.lenTagBuffer)
	chBlock := make(chan ebml.Block)
	chTimecode := make(chan uint64)
	go func() {
		defer func() {
			close(ch)
			cancel()
			res.Body.Close()
		}()
		var baseTime uint64
		for {
			select {
			case baseTime = <-chTimecode:
			case b, ok := <-chBlock:
				if !ok {
					return
				}
				ch <- &BlockWithBaseTimecode{
					Timecode: baseTime,
					Block:    b,
				}
			}
		}
	}()

	data := &Container{}
	data.Segment.Cluster.Timecode = chTimecode
	data.Segment.Cluster.SimpleBlock = chBlock
	data.Segment.Tags.Tag = chTag

	var errUnmarshal error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer func() {
			close(chBlock)
			close(chTag)
			wg.Done()
		}()
		errUnmarshal = ebml.Unmarshal(res.Body, data)
	}()

	return &blockReader{
		fnRead: func() (*BlockWithBaseTimecode, error) {
			b, ok := <-ch
			if !ok {
				return nil, io.EOF
			}
			return b, nil
		},
		fnReadTag: func() (*Tag, error) {
			t, ok := <-chTag
			if !ok {
				return nil, io.EOF
			}
			return t, nil
		},
		fnClose: func() (*Container, error) {
			cancel()
			wg.Wait()
			return data, errUnmarshal
		},
	}, nil
}

type StartSelector struct {
	AfterFragmentNumber string `json:",omitempty"`
	ContinuationToken   string `json:",omitempty"`
	StartSelectorType   StartSelectorType
	StartTimestamp      int `json:",omitempty"`
}

type GetMediaBody struct {
	StartSelector StartSelector
	StreamARN     *string `json:",omitempty"`
	StreamName    *string `json:",omitempty"`
}

type GetMediaOptions struct {
	startSelector  StartSelector
	lenTagBuffer   int
	lenBlockBuffer int
}

type GetMediaOption func(*GetMediaOptions)

func WithStartSelectorNow() GetMediaOption {
	return func(options *GetMediaOptions) {
		options.startSelector = StartSelector{
			StartSelectorType: StartSelectorTypeNow,
		}
	}
}

func WithStartSelectorProducerTimestamp(timestamp time.Time) GetMediaOption {
	return func(options *GetMediaOptions) {
		options.startSelector = StartSelector{
			StartSelectorType: StartSelectorTypeProducerTimestamp,
			StartTimestamp:    int(timestamp.Unix()),
		}
	}
}

func WithStartSelectorContinuationToken(token string) GetMediaOption {
	return func(options *GetMediaOptions) {
		options.startSelector = StartSelector{
			StartSelectorType: StartSelectorTypeContinuationToken,
			ContinuationToken: token,
		}
	}
}

func WithGetMediaBufferLen(n int) GetMediaOption {
	return func(options *GetMediaOptions) {
		options.lenBlockBuffer = n
	}
}

func WithGetMediaTagBufferLen(n int) GetMediaOption {
	return func(options *GetMediaOptions) {
		options.lenTagBuffer = n
	}
}
