# Migration guide

## v2

- aws/aws-sdk-go is updated to aws/aws-sdk-go-v2
  - 🔄Create kinesisvideomanager client with `aws.Config`
    ```diff
     import (
    -  "github.com/aws/aws-sdk-go/aws/session"
    -  kvm "github.com/seqsense/kinesisvideomanager"
    +  "github.com/aws/aws-sdk-go-v2/config"
    +  kvm "github.com/seqsense/kinesisvideomanager/v2"
     )

    -sess, err := session.NewSession()
    +cfg, err := config.LoadDefaultConfig(ctx)
     if err != nil {
       // error handling
     }
    -manager := kvm.New(sess)
    +manager := kvm.New(cfg)
    ```
  - 🔄Create mediafragment client with `aws.Config`
    ```diff
     import (
    -  "github.com/aws/aws-sdk-go/aws/session"
    -  "github.com/seqsense/kinesisvideomanager/mediafragment"
    +  "github.com/aws/aws-sdk-go-v2/config"
    +  "github.com/seqsense/kinesisvideomanager/v2/mediafragment"
     )

    -sess, err := session.NewSession()
    +cfg, err := config.LoadDefaultConfig(ctx)
     if err != nil {
       // error handling
     }
    -cli, err := mediafragment.New(kvm.StreamName(streamName), sess)
    +cli, err := mediafragment.New(ctx, kvm.StreamName(streamName), cfg)
    ```
- Non-immediate functions require context as a first argument
  - 🔄Pass ctx to `manager.Consumer()` and `manager.Provider()`
    ```diff
    -manager.Consumer(kvm.StreamName(streamName))
    +manager.Consumer(ctx, kvm.StreamName(streamName))
    ```
    ```diff
    -manager.Provider(kvm.StreamName(streamName), trackEntries)
    +manager.Provider(ctx, kvm.StreamName(streamName), trackEntries)
    ```
  - 🔄Pass ctx to `mediaflagment.New()`
    ```diff
    -list, err := cli.ListFragments(listOptions...)
    +list, err := cli.ListFragments(ctx, listOptions...)
    ```
    ```diff
	-err := cli.GetMediaForFragmentList(fragmentIDs, cbFragment, cbError)
	+err := cli.GetMediaForFragmentList(context.Background(), fragmentIDs, cbFragment, cbError)
    ```
