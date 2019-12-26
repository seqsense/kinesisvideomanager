package kinesisvideomanager

import (
	"github.com/at-wat/ebml-go"
)

type BlockWithBaseTimecode struct {
	Timecode uint64
	Block    ebml.Block
}

type BlockChWithBaseTime struct {
	Timecode uint64
	Block    chan ebml.Block
	Tag      chan Tag
}
