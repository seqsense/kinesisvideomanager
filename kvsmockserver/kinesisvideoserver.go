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
	"sync"
	"time"

	"github.com/at-wat/ebml-go"
	kvm "github.com/seqsense/kinesisvideomanager"
)

type KinesisVideoServer struct {
	*httptest.Server
	fragments map[uint64]FragmentTest
	blockTime time.Duration
	mu        sync.Mutex

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
	s.Server = httptest.NewServer(mux)
	return s
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

type TagsTest struct {
	Tag []kvm.Tag
}
