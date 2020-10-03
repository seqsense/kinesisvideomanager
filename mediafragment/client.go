package mediafragment

import (
	"fmt"
	"sync"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"
	kvam "github.com/aws/aws-sdk-go/service/kinesisvideoarchivedmedia"
	kvm "github.com/seqsense/kinesisvideomanager"
)

type Client struct {
	streamID                      kvm.StreamID
	clientListFragments           *kvam.KinesisVideoArchivedMedia
	clientGetMediaForFragmentList *kvam.KinesisVideoArchivedMedia
}

type FragmentError struct {
	FragmentNumber string
	Code           string
	Message        string
}

func (e *FragmentError) Error() string {
	return fmt.Sprintf("fragmentNumber:%s code:%s message:%s", e.FragmentNumber, e.Code, e.Message)
}

func New(streamID kvm.StreamID, sess client.ConfigProvider, cfgs ...*aws.Config) (*Client, error) {
	kv := kinesisvideo.New(sess, cfgs...)

	ep, err := kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String(kinesisvideo.APINameListFragments),
			StreamName: streamID.StreamName(),
		},
	)
	if err != nil {
		return nil, err
	}
	clientListFragments := kvam.New(sess, aws.NewConfig().WithEndpoint(*ep.DataEndpoint))

	ep, err = kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String(kinesisvideo.APINameGetMediaForFragmentList),
			StreamName: streamID.StreamName(),
		},
	)
	if err != nil {
		return nil, err
	}
	clientGetMediaForFragmentList := kvam.New(sess, aws.NewConfig().WithEndpoint(*ep.DataEndpoint))

	return &Client{
		streamID:                      streamID,
		clientListFragments:           clientListFragments,
		clientGetMediaForFragmentList: clientGetMediaForFragmentList,
	}, nil
}

func (c *Client) ListFragments(opts ...ListFragmentsOption) (*ListFragmentsOutput, error) {
	input := &kvam.ListFragmentsInput{
		StreamName: c.streamID.StreamName(),
	}
	for _, o := range opts {
		o(input)
	}

	req, out := c.clientListFragments.ListFragmentsRequest(input)
	if err := req.Send(); err != nil {
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

func (c *Client) GetMediaForFragmentList(fragments FragmentIDs, handler func(kvm.Fragment), errHandler func(error)) error {
	req, out := c.clientGetMediaForFragmentList.GetMediaForFragmentListRequest(&kvam.GetMediaForFragmentListInput{
		Fragments:  fragments,
		StreamName: c.streamID.StreamName(),
	})
	if err := req.Send(); err != nil {
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
		input.FragmentSelector = &kvam.FragmentSelector{
			FragmentSelectorType: aws.String(kvam.FragmentSelectorTypeServerTimestamp),
			TimestampRange: &kvam.TimestampRange{
				StartTimestamp: aws.Time(startTime),
				EndTimestamp:   aws.Time(endTime),
			},
		}
	}
}

func WithProducerTimestampRange(startTime, endTime time.Time) ListFragmentsOption {
	return func(input *kvam.ListFragmentsInput) {
		input.FragmentSelector = &kvam.FragmentSelector{
			FragmentSelectorType: aws.String(kvam.FragmentSelectorTypeProducerTimestamp),
			TimestampRange: &kvam.TimestampRange{
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
