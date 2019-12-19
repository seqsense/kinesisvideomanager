package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/kinesisvideo"
)

const (
	streamName = "test-stream"
)

func main() {
	log.SetFlags(log.Lmicroseconds)

	sess := session.Must(session.NewSession())

	config := aws.NewConfig()
	kv := kinesisvideo.New(sess, config)
	cliConfig := sess.ClientConfig("kinesisvideo")

	ep, err := kv.GetDataEndpoint(
		&kinesisvideo.GetDataEndpointInput{
			APIName:    aws.String("GET_MEDIA"),
			StreamName: aws.String(streamName),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("endpoint: %s", *ep.DataEndpoint)

	cred, err := cliConfig.Config.Credentials.Get()
	if err != nil {
		log.Fatal(err)
	}

	signer := v4.NewSigner(
		credentials.NewStaticCredentials(cred.AccessKeyID, cred.SecretAccessKey, ""),
	)

	type StartSelector struct {
		AfterFragmentNumber string `json:",omitempty"`
		ContinuationToken   string `json:",omitempty"`
		StartSelectorType   string
		StartTimestamp      int `json:",omitempty"`
	}
	type GetMediaBody struct {
		StartSelector StartSelector
		StreamARN     string `json:",omitempty"`
		StreamName    string `json:",omitempty"`
	}
	body, err := json.Marshal(
		&GetMediaBody{
			StartSelector: StartSelector{
				StartSelectorType: "NOW",
			},
			StreamName: streamName,
		})
	if err != nil {
		log.Fatal(err)
	}
	epFull := *ep.DataEndpoint + "/getMedia"
	bodyReader := bytes.NewReader(body)

	req, err := http.NewRequest("POST", epFull, bodyReader)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("X-Amz-Security-Token", cred.SessionToken)

	_, err = signer.Presign(
		req, bodyReader,
		cliConfig.SigningName, cliConfig.SigningRegion,
		time.Hour, time.Now(),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Print("send request")
	var client http.Client
	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	for {
		b := make([]byte, 4096)
		n, err := res.Body.Read(b)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("media: %v", b[:n])
	}
}
