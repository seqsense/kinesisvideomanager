package mediafragment

import (
	"sort"

	kvam "github.com/aws/aws-sdk-go/service/kinesisvideoarchivedmedia"
)

type ListFragmentsOutput struct {
	*kvam.ListFragmentsOutput
}

type FragmentIDs []*string

func NewFragmentIDs(ids ...string) FragmentIDs {
	var ret FragmentIDs
	for _, id := range ids {
		ret = append(ret, &id)
	}
	return ret
}

func (l *ListFragmentsOutput) Sort() {
	sort.Sort(l)
}

func (l *ListFragmentsOutput) Len() int {
	return len(l.Fragments)
}

func (l *ListFragmentsOutput) Swap(i, j int) {
	l.Fragments[i], l.Fragments[j] = l.Fragments[j], l.Fragments[i]
}

func (l *ListFragmentsOutput) Less(i, j int) bool {
	return *l.Fragments[i].FragmentNumber < *l.Fragments[j].FragmentNumber
}

func (l *ListFragmentsOutput) FragmentIDs() FragmentIDs {
	var ret FragmentIDs
	for _, f := range l.Fragments {
		ret = append(ret, f.FragmentNumber)
	}
	return ret
}
