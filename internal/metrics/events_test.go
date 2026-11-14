package metrics

import "testing"

func TestEventValidation(t *testing.T) {
	valid := Event{Name: QuestionAsked, Metadata: map[string]string{"surface": "chat"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if err := (Event{Name: "secret_dump"}).Validate(); err == nil {
		t.Fatal("unknown event accepted")
	}
}
