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

package kinesisvideomanager

import (
	"github.com/at-wat/ebml-go"
)

type Container struct {
	Header  EBMLHeader `ebml:"EBML"`
	Segment Segment    `ebml:",size=unknown"`
}

type EBMLHeader struct {
	EBMLVersion            uint64
	EBMLReadVersion        uint64
	EBMLMaxIDLength        uint64
	EBMLMaxSizeLength      uint64
	EBMLDocType            string
	EBMLDocTypeVersion     uint64
	EBMLDocTypeReadVersion uint64
}
type Info struct {
	TimecodeScale   uint64
	SegmentUID      []byte
	SegmentFilename string
	Title           string
	MuxingApp       string
	WritingApp      string
}
type TrackEntry struct {
	Name        string
	TrackNumber uint64
	TrackUID    uint64
	CodecID     string
	CodecName   string
	TrackType   uint64
}
type Tracks struct {
	TrackEntry []TrackEntry
}
type Cluster struct {
	Timecode    chan uint64
	Position    uint64 `ebml:",omitempty"`
	SimpleBlock chan ebml.Block
}
type ClusterWrite struct {
	Timecode    chan uint64
	Position    uint64 `ebml:",omitempty"`
	SimpleBlock chan ebml.Block
}
type SimpleTag struct {
	TagName   string
	TagString string `ebml:",omitempty"`
	TagBinary string `ebml:",omitempty"`
}
type Tag struct {
	SimpleTag []SimpleTag
}
type Tags struct {
	Tag chan *Tag `ebml:",omitempty"`
}
type Segment struct {
	Info    Info
	Tracks  Tracks
	Cluster Cluster `ebml:",size=unknown"`
	Tags    Tags
}
type SegmentWrite struct {
	Info    Info
	Tracks  Tracks
	Cluster ClusterWrite `ebml:",size=unknown"`
	Tags    Tags
}
