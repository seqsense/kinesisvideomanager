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
	Timecode    uint64
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
	Tag chan Tag `ebml:",omitempty"`
}
type Segment struct {
	Info    Info
	Tracks  Tracks
	Cluster Cluster `ebml:",size=unknown"`
	Tags    Tags
}
