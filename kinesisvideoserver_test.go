package kinesisvideomanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/at-wat/ebml-go"
)

type KinesisVideoServer struct {
	*httptest.Server
	fragments map[uint64]FragmentTest
}

func NewKinesisVideoServer() *KinesisVideoServer {
	s := &KinesisVideoServer{
		fragments: make(map[uint64]FragmentTest),
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
		Header  EBMLHeader `ebml:"EBML"`
		Segment segment    `ebml:",size=unknown"`
	}{}

	timecodeType := FragmentTimecodeType(r.Header.Get("x-amzn-fragment-timecode-type"))
	baseTimecode := uint64(0)
	if timecodeType == FragmentTimecodeTypeRelative {
		startTimestamp := r.Header.Get("x-amzn-producer-start-timestamp")
		ts, err := ParseTimestamp(startTimestamp)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "%v", err)
			return
		}
		baseTimecode = uint64(ts.UnixNano() / int64(time.Millisecond))
	}

	if err := ebml.Unmarshal(r.Body, data); err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "%v", err)
		return
	}

	data.Segment.Cluster.Timecode += baseTimecode
	s.fragments[data.Segment.Cluster.Timecode] = FragmentTest{
		Cluster: data.Segment.Cluster,
		Tags:    data.Segment.Tags,
	}

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
	body := &GetMediaBody{}
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
			Header  EBMLHeader `ebml:"EBML"`
			Segment segment    `ebml:",size=unknown"`
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
	Info    Info
	Tracks  Tracks
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
	Tag []Tag
}
