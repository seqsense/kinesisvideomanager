package mediafragment

import (
	"sort"

	kvam "github.com/aws/aws-sdk-go/service/kinesisvideoarchivedmedia"
)

type listFragmentsOutput struct {
	*kvam.ListFragmentsOutput
}

func (l *listFragmentsOutput) Sort() {
	sort.Sort(l)
}

func (l *listFragmentsOutput) Len() int {
	return len(l.Fragments)
}

func (l *listFragmentsOutput) Swap(i, j int) {
	l.Fragments[i], l.Fragments[j] = l.Fragments[j], l.Fragments[i]
}

func (l *listFragmentsOutput) Less(i, j int) bool {
	return *l.Fragments[i].FragmentNumber < *l.Fragments[j].FragmentNumber
}

func (l *listFragmentsOutput) FragmentList() []*string {
	var ret []*string
	for _, f := range l.Fragments {
		ret = append(ret, f.FragmentNumber)
	}
	return ret
}
