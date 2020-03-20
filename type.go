package kinesisvideomanager

import (
	"github.com/at-wat/ebml-go"
)

type BlockWithBaseTimecode struct {
	Timecode uint64
	Block    ebml.Block
}

func (bt *BlockWithBaseTimecode) AbsTimecode() int64 {
	return int64(bt.Timecode) + int64(bt.Block.Timecode)
}

type BlockChWithBaseTimecode struct {
	Timecode chan uint64
	Block    chan ebml.Block
	Tag      chan *Tag
}
