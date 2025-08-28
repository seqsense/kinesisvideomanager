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

package mediafragment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kinesisvideo_types "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	kvam "github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia"
	kvam_types "github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia/types"
	kvm "github.com/seqsense/kinesisvideomanager"
)

type Client struct {
	streamID                      kvm.StreamID
	clientListFragments           *kvam.Client
	clientGetMediaForFragmentList *kvam.Client
}

type FragmentError struct {
	FragmentNumber string
	Code           string
	Message        string
}

func (e *FragmentError) Error() string {
	return fmt.Sprintf("fragmentNumber:%s code:%s message:%s", e.FragmentNumber, e.Code, e.Message)
}

// New creates a KVS media fragment client.
// Passed context is used only to get the KVS data endpint
// and does not control the future data read session.
func New(ctx context.Context, streamID kvm.StreamID, cfg aws.Config) (*Client, error) {
	kv := kinesisvideo.NewFromConfig(cfg)

	epListFragments, err := kv.GetDataEndpoint(ctx,
		&kinesisvideo.GetDataEndpointInput{
			APIName:    kinesisvideo_types.APINameListFragments,
			StreamName: streamID.StreamName(),
		},
	)
	if err != nil {
		return nil, err
	}
	clientListFragments := kvam.NewFromConfig(cfg, func(o *kvam.Options) {
		o.BaseEndpoint = epListFragments.DataEndpoint
	})

	epGetMediaForFragmentList, err := kv.GetDataEndpoint(ctx,
		&kinesisvideo.GetDataEndpointInput{
			APIName:    kinesisvideo_types.APINameGetMediaForFragmentList,
			StreamName: streamID.StreamName(),
		},
	)
	if err != nil {
		return nil, err
	}
	clientGetMediaForFragmentList := kvam.NewFromConfig(cfg, func(o *kvam.Options) {
		o.BaseEndpoint = epGetMediaForFragmentList.DataEndpoint
	})

	return &Client{
		streamID:                      streamID,
		clientListFragments:           clientListFragments,
		clientGetMediaForFragmentList: clientGetMediaForFragmentList,
	}, nil
}

func (c *Client) ListFragments(ctx context.Context, opts ...ListFragmentsOption) (*ListFragmentsOutput, error) {
	input := &kvam.ListFragmentsInput{
		StreamName: c.streamID.StreamName(),
	}
	for _, o := range opts {
		o(input)
	}

	out, err := c.clientListFragments.ListFragments(ctx, input)
	if err != nil {
		return nil, err
	}

	/*
	 * Sort fragments because they are not sorted.
	 * see: https://docs.aws.amazon.com/kinesisvideostreams/latest/dg/API_reader_ListFragments.html#API_reader_ListFragments_ResponseElements
	 *  > Results are in no specific order, even across pages.
	 */
	ret := ListFragmentsOutput{ListFragmentsOutput: out}
	ret.Sort()
	return &ret, nil
}

func (c *Client) GetMediaForFragmentList(ctx context.Context, fragments FragmentIDs, handler func(kvm.Fragment), errHandler func(error)) error {
	out, err := c.clientGetMediaForFragmentList.GetMediaForFragmentList(ctx, &kvam.GetMediaForFragmentListInput{
		Fragments:  fragments,
		StreamName: c.streamID.StreamName(),
	})
	if err != nil {
		return err
	}

	chBlock := make(chan ebml.Block)
	chTimecode := make(chan uint64)
	chTag := make(chan *kvm.Tag)
	var fragment kvm.Fragment
	done := sync.WaitGroup{}
	done.Add(1)
	go func() {
		defer func() {
			if len(fragment) > 0 {
				handler(fragment)
			}
			done.Done()
		}()

		var metadata *kvm.FragmentMetadata
		var baseTimecode uint64
		for {
			select {
			case tag := <-chTag:
				switch tag.SimpleTag[0].TagName {
				case kvm.TagNameFragmentNumber:
					// start new fragment
					if metadata != nil {
						handler(fragment)
						fragment = nil
					}

					metadata = &kvm.FragmentMetadata{}
					var err *FragmentError
					for _, t := range tag.SimpleTag {
						switch t.TagName {
						case kvm.TagNameFragmentNumber:
							metadata.FragmentNumber = t.TagString
						case kvm.TagNameServerTimestamp:
							ts, err := kvm.ParseTimestamp(t.TagString)
							if err != nil {
								errHandler(fmt.Errorf("failed to parse server timestamp (%s): %w", t.TagString, err))
							}
							metadata.ServerTimestamp = ts
						case kvm.TagNameProducerTimestamp:
							ts, err := kvm.ParseTimestamp(t.TagString)
							if err != nil {
								errHandler(fmt.Errorf("failed to parse producer timestamp (%s): %w", t.TagString, err))
							}
							metadata.ProducerTimestamp = ts
						case kvm.TagNameExceptionErrorCode:
							if err == nil {
								err = &FragmentError{
									FragmentNumber: metadata.FragmentNumber,
								}
							}
							err.Code = t.TagString
						case kvm.TagNameExceptionMessage:
							if err == nil {
								err = &FragmentError{
									FragmentNumber: metadata.FragmentNumber,
								}
							}
							err.Message = t.TagString
						}
					}
					if err != nil {
						errHandler(err)
					}
				default:
					// Set custom tags
					metadata.Tags = make(map[string]kvm.SimpleTag)
					for _, t := range tag.SimpleTag {
						metadata.Tags[t.TagName] = t
					}
				}
			case baseTimecode = <-chTimecode:
			case block, ok := <-chBlock:
				if !ok {
					return
				}
				b := &kvm.BlockWithMetadata{
					FragmentMetadata: metadata,
					BlockWithBaseTimecode: &kvm.BlockWithBaseTimecode{
						Timecode: baseTimecode,
						Block:    block,
					},
				}
				fragment = append(fragment, b)
			}
		}
	}()

	data := &kvm.Container{}
	data.Segment.Cluster.Timecode = chTimecode
	data.Segment.Cluster.SimpleBlock = chBlock
	data.Segment.Tags.Tag = chTag
	if err := ebml.Unmarshal(out.Payload, data); err != nil {
		return err
	}
	close(chBlock)
	done.Wait()
	return nil
}

type ListFragmentsOption func(input *kvam.ListFragmentsInput)

func WithNextToken(nextToken *string) ListFragmentsOption {
	return func(input *kvam.ListFragmentsInput) {
		input.NextToken = nextToken
	}
}

func WithServerTimestampRange(startTime, endTime time.Time) ListFragmentsOption {
	return func(input *kvam.ListFragmentsInput) {
		input.FragmentSelector = &kvam_types.FragmentSelector{
			FragmentSelectorType: kvam_types.FragmentSelectorTypeServerTimestamp,
			TimestampRange: &kvam_types.TimestampRange{
				StartTimestamp: aws.Time(startTime),
				EndTimestamp:   aws.Time(endTime),
			},
		}
	}
}

func WithProducerTimestampRange(startTime, endTime time.Time) ListFragmentsOption {
	return func(input *kvam.ListFragmentsInput) {
		input.FragmentSelector = &kvam_types.FragmentSelector{
			FragmentSelectorType: kvam_types.FragmentSelectorTypeProducerTimestamp,
			TimestampRange: &kvam_types.TimestampRange{
				StartTimestamp: aws.Time(startTime),
				EndTimestamp:   aws.Time(endTime),
			},
		}
	}
}

func WithMaxResults(maxResults int64) ListFragmentsOption {
	return func(input *kvam.ListFragmentsInput) {
		input.MaxResults = aws.Int64(maxResults)
	}
}
