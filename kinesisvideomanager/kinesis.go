package kinesisvideomanager

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
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

	cred, err := cliConfig.Config.Credentials.Get()
	if err != nil {
		return nil, err
	}

	return &Client{
		kv: kinesisvideo.New(sess, cfgs...),
		signer: v4.NewSigner(
			credentials.NewStaticCredentials(
				cred.AccessKeyID,
				cred.SecretAccessKey,
				cred.SessionToken,
			),
		),
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
