package kinesisvideomanager

type StartSelectorType string

const (
	StartSelectorTypeNow               StartSelectorType = "NOW"
	StartSelectorTypeEarliest          StartSelectorType = "EARLIEST"
	StartSelectorTypeFragmentNumber    StartSelectorType = "FRAGMENT_NUMBER"
	StartSelectorTypeProducerTimestamp StartSelectorType = "PRODUCER_TIMESTAMP"
	StartSelectorTypeServerTimestamp   StartSelectorType = "SERVER_TIMESTAMP"
	StartSelectorTypeContinuationToken StartSelectorType = "CONTINUATION_TOKEN"
)
