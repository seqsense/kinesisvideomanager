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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

func (c *Consumer) GetMedia(ch chan *BlockWithBaseTimecode, chTag chan *Tag, opts ...GetMediaOption) (*Container, error) {
	defer func() {
		close(ch)
		close(chTag)
	}()

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

	req, err := http.NewRequest("POST", c.endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-type", "application/json")

	_, err = c.signer.Presign(
		req, bodyReader,
		c.cliConfig.SigningName, c.cliConfig.SigningRegion,
		10*time.Minute, time.Now(),
	)
	if err != nil {
		return nil, err
	}
	res, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%d: %s", res.StatusCode, string(body))
	}

	chBlock := make(chan ebml.Block)
	chTimecode := make(chan uint64)
	go func() {
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
	if err := ebml.Unmarshal(res.Body, data); err != nil {
		return nil, err
	}
	return data, nil
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
	startSelector StartSelector
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
