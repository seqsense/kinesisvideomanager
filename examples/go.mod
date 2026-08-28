module github.com/seqsense/kinesisvideomanager/v2/examples

go 1.24

require (
	github.com/at-wat/ebml-go v0.19.1
	github.com/aws/aws-sdk-go-v2 v1.44.0
	github.com/aws/aws-sdk-go-v2/config v1.32.40
	github.com/aws/aws-sdk-go-v2/credentials v1.19.39
	github.com/aws/aws-sdk-go-v2/service/kinesisvideo v1.36.8
	github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia v1.36.8
	github.com/google/uuid v1.6.0
	github.com/seqsense/kinesisvideomanager/v2 v2.0.1
	github.com/seqsense/sq-gst-go v0.5.4
)

require (
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.40 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.40 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.40 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.41 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.40 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.6.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.34.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.39.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.46.0 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
)

replace github.com/seqsense/kinesisvideomanager/v2 => ../
