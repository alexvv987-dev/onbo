package metrics

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	PlanOpened       = "plan_opened"
	TaskCompleted    = "task_completed"
	QuestionAsked    = "question_asked"
	SourceShown      = "source_shown"
	DraftCreated     = "draft_created"
	RequestConfirmed = "request_confirmed"
	RequestResult    = "request_result"
	RequestRefreshed = "request_refreshed"
)

var allowedEvents = map[string]struct{}{
	PlanOpened: {}, TaskCompleted: {}, QuestionAsked: {}, SourceShown: {},
	DraftCreated: {}, RequestConfirmed: {}, RequestResult: {}, RequestRefreshed: {},
}

type Event struct {
	Name     string
	Metadata map[string]string
}

func (event Event) Validate() error {
	if _, ok := allowedEvents[event.Name]; !ok {
		return fmt.Errorf("unknown event %q", event.Name)
	}
	if len(event.Metadata) > 8 {
		return fmt.Errorf("too many event properties")
	}
	encoded, err := json.Marshal(event.Metadata)
	if err != nil || len(encoded) > 2048 {
		return fmt.Errorf("event properties are too large")
	}
	return nil
}

type Recorder interface {
	Record(context.Context, string, Event) error
}
