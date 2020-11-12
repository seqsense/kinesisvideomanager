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
	"time"

	"github.com/at-wat/ebml-go"
)

type Fragment []*BlockWithMetadata

type BlockWithMetadata struct {
	*BlockWithBaseTimecode
	*FragmentMetadata
}

type FragmentMetadata struct {
	FragmentNumber    string
	ProducerTimestamp time.Time
	ServerTimestamp   time.Time
	Tags              map[string]SimpleTag
}

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
