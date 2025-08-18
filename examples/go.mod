module github.com/seqsense/kinesisvideomanager/examples

go 1.25

require (
	github.com/at-wat/ebml-go v0.17.1
	github.com/aws/aws-sdk-go v1.55.8
	github.com/seqsense/kinesisvideomanager v0.0.0
	github.com/seqsense/sq-gst-go v0.5.3
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
)

replace github.com/seqsense/kinesisvideomanager => ../
