package examples

// **This file is not needed if you import kinesisvideomanager normally**

// Directly specify the indirect dependencies from ../
// to workaround the problem that go mod tidy is not executed by Renovate.
// https://github.com/renovatebot/renovate/issues/12999

import (
	_ "github.com/aws/aws-sdk-go-v2"
	_ "github.com/aws/aws-sdk-go-v2/credentials"
	_ "github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	_ "github.com/google/uuid"
)
