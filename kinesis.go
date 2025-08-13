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
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
)

const (
	presignServiceName = "kinesisvideo"
	presignExpires     = 10 * time.Minute
)

type Client struct {
	kv        *kinesisvideo.Client
	signer    v4.HTTPSigner
	cliConfig aws.Config
}

func New(cfg aws.Config) (*Client, error) {
	return &Client{
		kv:        kinesisvideo.NewFromConfig(cfg),
		signer:    v4.NewSigner(),
		cliConfig: cfg,
	}, nil
}

func (c *Client) sign(ctx context.Context, req *http.Request, payload []byte) error {
	cred, err := c.cliConfig.Credentials.Retrieve(ctx)
	if err != nil {
		return err
	}

	query := req.URL.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(presignExpires/time.Second), 10))
	req.URL.RawQuery = query.Encode()

	h := sha256.Sum256(payload)
	hs := hex.EncodeToString(h[:])

	if err := c.signer.SignHTTP(
		ctx, cred, req, hs,
		presignServiceName, c.cliConfig.Region,
		time.Now(),
	); err != nil {
		return err
	}
	return nil
}

type StreamID interface {
	StreamARN() *string
	StreamName() *string
}

type streamID struct {
	arn  *string
	name *string
}

func StreamARN(arn string) StreamID {
	return &streamID{
		arn: &arn,
	}
}

func StreamName(name string) StreamID {
	return &streamID{
		name: &name,
	}
}

func (s *streamID) StreamARN() *string {
	return s.arn
}

func (s *streamID) StreamName() *string {
	return s.name
}

func (s *streamID) String() string {
	if s.name != nil {
		return *s.name
	}
	if s.arn != nil {
		return *s.arn
	}
	return "invalid_stream_id"
}
