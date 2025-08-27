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

package kvsmockserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/aws/aws-sdk-go-v2/aws"

	kvm "github.com/seqsense/kinesisvideomanager"
)

type KinesisVideoServer struct {
	*httptest.Server
	fragments map[uint64]FragmentTest
	blockTime time.Duration
	mu        sync.Mutex

	producerTimestampOrigin float64
	serverTimestampOrigin   float64

	putMediaHook func(uint64, *FragmentTest, http.ResponseWriter) bool
}

type KinesisVideoServerOption func(*KinesisVideoServer)

func WithBlockTime(blockTime time.Duration) KinesisVideoServerOption {
	return func(s *KinesisVideoServer) {
		s.blockTime = blockTime
	}
}

func WithPutMediaHook(h func(uint64, *FragmentTest, http.ResponseWriter) bool) KinesisVideoServerOption {
	return func(s *KinesisVideoServer) {
		s.putMediaHook = h
	}
}

func WithTimestampOrigin(producer, server float64) KinesisVideoServerOption {
	return func(s *KinesisVideoServer) {
		s.producerTimestampOrigin = producer
		s.serverTimestampOrigin = server
	}
}

func NewKinesisVideoServer(opts ...KinesisVideoServerOption) *KinesisVideoServer {
	s := &KinesisVideoServer{
		fragments: make(map[uint64]FragmentTest),
	}
	for _, opt := range opts {
		opt(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/getDataEndpoint", s.getDataEndpoint)
	mux.HandleFunc("/putMedia", s.putMedia)
	mux.HandleFunc("/getMedia", s.getMedia)
	mux.HandleFunc("/listFragments", s.listFragments)
	mux.HandleFunc("/getMediaForFragmentList", s.getMediaForFragmentList)
	s.Server = httptest.NewServer(mux)
	return s
}

func fragmentNumber(fragment FragmentTest) string {
	return fmt.Sprintf("%d", fragment.Cluster.Timecode)
}

func (s *KinesisVideoServer) GetFragment(timecode uint64) (FragmentTest, bool) {
	fragment, ok := s.fragments[timecode]
	return fragment, ok
}

func (s *KinesisVideoServer) RegisterFragment(fragment FragmentTest) {
	s.fragments[fragment.Cluster.Timecode] = fragment
}

func (s *KinesisVideoServer) getDataEndpoint(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `{"DataEndpoint": "%s"}`, s.URL)
}

func (s *KinesisVideoServer) putMedia(w http.ResponseWriter, r *http.Request) {
	data := &struct {
		Header  kvm.EBMLHeader `ebml:"EBML"`
		Segment segment        `ebml:",size=unknown"`
	}{}

	timecodeType := kvm.FragmentTimecodeType(r.Header.Get("x-amzn-fragment-timecode-type"))
	baseTimecode := uint64(0)
	if timecodeType == kvm.FragmentTimecodeTypeRelative {
		startTimestamp := r.Header.Get("x-amzn-producer-start-timestamp")
		ts, err := kvm.ParseTimestamp(startTimestamp)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "%v", err)
			return
		}
		baseTimecode = uint64(ts.UnixNano() / int64(time.Millisecond))
	}

	time.Sleep(s.blockTime)
	if err := ebml.Unmarshal(r.Body, data); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}

	data.Segment.Cluster.Timecode += baseTimecode
	fragment := FragmentTest{
		Cluster: data.Segment.Cluster,
		Tags:    data.Segment.Tags,
	}
	if s.putMediaHook != nil {
		if !s.putMediaHook(data.Segment.Cluster.Timecode, &fragment, w) {
			return
		}
	}
	s.mu.Lock()
	s.fragments[data.Segment.Cluster.Timecode] = fragment
	s.mu.Unlock()

	fmt.Fprintf(w,
		`{"EventType":"PERSISTED", "FragmentTimecode":%d, "FragmentNumber":"%s"}`,
		baseTimecode+data.Segment.Cluster.Timecode, "12345678901234567890123456789012345678901234567",
	)
}

func (s *KinesisVideoServer) getMedia(w http.ResponseWriter, r *http.Request) {
	bs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}
	body := &kvm.GetMediaBody{}
	if err := json.Unmarshal(bs, body); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}
	startTimestamp := uint64(body.StartSelector.StartTimestamp)

	buf := bytes.NewBuffer(nil)
	for _, fragment := range s.fragments {
		if fragment.Cluster.Timecode < startTimestamp {
			continue
		}

		data := &struct {
			Header  kvm.EBMLHeader `ebml:"EBML"`
			Segment segment        `ebml:",size=unknown"`
		}{}
		data.Segment.Cluster = fragment.Cluster
		data.Segment.Tags = fragment.Tags
		if err := ebml.Marshal(data, buf); err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "%v", err)
			return
		}
	}
	w.Write(buf.Bytes())
}

func (s *KinesisVideoServer) listFragments(w http.ResponseWriter, r *http.Request) {
	body := &listFragmentsInput{}
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}

	out := &listFragmentsOutput{
		Fragments: []fragment{},
	}
	for _, f := range s.fragments {
		if body.FragmentSelector != nil {
			var ts float64
			switch body.FragmentSelector.FragmentSelectorType {
			case "PRODUCER_TIMESTAMP":
				ts = float64(f.Cluster.Timecode)/1000 + s.producerTimestampOrigin
			case "SERVER_TIMESTAMP":
				ts = float64(f.Cluster.Timecode)/1000 + s.serverTimestampOrigin
			default:
				w.WriteHeader(500)
				fmt.Fprintf(w, "invalid FragmentSelectorType")
				return
			}
			tr := body.FragmentSelector.TimestampRange
			if tr.StartTimestamp != nil && ts < *tr.StartTimestamp {
				continue
			}
			if tr.EndTimestamp != nil && ts > *tr.EndTimestamp {
				continue
			}
		}
		out.Fragments = append(out.Fragments, fragment{
			FragmentNumber:    aws.String(fmt.Sprintf("%d", f.Cluster.Timecode)),
			ProducerTimestamp: aws.Float64(float64(f.Cluster.Timecode / 1000)),
			ServerTimestamp:   aws.Float64(float64(f.Cluster.Timecode / 1000)),
		})
	}

	if err := json.NewEncoder(w).Encode(out); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}
}

func (s *KinesisVideoServer) getMediaForFragmentList(w http.ResponseWriter, r *http.Request) {
	body := &getMediaForFragmentListInput{}
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}

	// Validate request
	for _, id := range body.Fragments {
		timecode, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "invalid fragment number")
			return
		}
		if _, ok := s.fragments[timecode]; !ok {
			w.WriteHeader(400)
			fmt.Fprintf(w, "unknown fragment number")
			return
		}
	}

	header := &struct {
		Header  kvm.EBMLHeader `ebml:"EBML"`
		Segment segment        `ebml:",size=unknown"`
	}{}
	if err := ebml.Marshal(header, w); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}

	for _, id := range body.Fragments {
		timecode, _ := strconv.ParseUint(id, 10, 64)
		fragment := s.fragments[timecode]

		data := &struct {
			Cluster ClusterTest
			Tags    TagsTest
		}{
			Cluster: fragment.Cluster,
			Tags: TagsTest{
				Tag: append(fragment.Tags.Tag, kvm.Tag{
					SimpleTag: []kvm.SimpleTag{{
						TagName:   kvm.TagNameFragmentNumber,
						TagString: id,
					}},
				}),
			},
		}
		if err := ebml.Marshal(data, w); err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "%v", err)
			return
		}
	}
}

type segment struct {
	Info    kvm.Info
	Tracks  kvm.Tracks
	Cluster ClusterTest `ebml:",size=unknown"`
	Tags    TagsTest
}

type FragmentTest struct {
	Cluster ClusterTest
	Tags    TagsTest
}

type ClusterTest struct {
	Timecode    uint64
	Position    uint64 `ebml:",omitempty"`
	SimpleBlock []ebml.Block
}

type ClusterTestChan struct {
	Timecode    uint64
	Position    uint64 `ebml:",omitempty"`
	SimpleBlock []ebml.Block
}

type TagsTest struct {
	Tag []kvm.Tag
}
