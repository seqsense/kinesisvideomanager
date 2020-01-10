package kinesisvideomanager

import (
	"encoding/json"
	"io"
)

type FragmentEvent struct {
	EventType        string
	FragmentTimecode uint64
	FragmentNumber   string // 158-bit number, handle as string
}

func parseFragmentEvent(r io.Reader) ([]FragmentEvent, error) {
	dec := json.NewDecoder(r)
	var ret []FragmentEvent
	for {
		var fe FragmentEvent
		if err := dec.Decode(&fe); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		ret = append(ret, fe)
	}
	return ret, nil
}
