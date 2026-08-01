module github.com/seqsense/kinesisvideomanager/v2/examples

go 1.24

require (
	github.com/at-wat/ebml-go v0.18.0
	github.com/aws/aws-sdk-go-v2 v1.43.3
	github.com/aws/aws-sdk-go-v2/config v1.32.34
	github.com/aws/aws-sdk-go-v2/credentials v1.19.33
	github.com/aws/aws-sdk-go-v2/service/kinesisvideo v1.36.3
	github.com/aws/aws-sdk-go-v2/service/kinesisvideoarchivedmedia v1.36.3
	github.com/google/uuid v1.6.0
	github.com/seqsense/kinesisvideomanager/v2 v2.0.0
	github.com/seqsense/sq-gst-go v0.5.4
)

require (
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.35 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.3 // indirect
	github.com/aws/smithy-go v1.27.6 // indirect
)

replace github.com/seqsense/kinesisvideomanager/v2 => ../
