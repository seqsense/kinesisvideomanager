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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"
)

type Client struct {
	kv        *kinesisvideo.KinesisVideo
	signer    *v4.Signer
	cliConfig *client.Config
}

func New(sess client.ConfigProvider, cfgs ...*aws.Config) (*Client, error) {
	cliConfig := sess.ClientConfig("kinesisvideo")

	return &Client{
		kv:        kinesisvideo.New(sess, cfgs...),
		signer:    v4.NewSigner(cliConfig.Config.Credentials),
		cliConfig: &cliConfig,
	}, nil
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
