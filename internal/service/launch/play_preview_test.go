package launch

import (
	model "retrom/internal/model/launch"
	"testing"
)

func TestPreviewEventsNeverCreateProductTimeOrPlayEvents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state, kind string
		event             model.PlayEvent
		writes            int
		want              string
	}{
		{"start", "ACTIVE", "start", model.PlayEvent{}, 0, "ACTIVE"},
		{"heartbeat", "ACTIVE", "heartbeat", model.PlayEvent{ClientSequence: 87, PreviousInterval: &model.Interval{Running: true, Visible: true}}, 0, "ACTIVE"},
		{"finish loading", "CREATED", "finish", model.PlayEvent{}, 1, "FINISHED"},
		{"finish active", "ACTIVE", "finish", model.PlayEvent{ClientSequence: 4, PreviousInterval: &model.Interval{}}, 1, "FINISHED"},
		{"repeat finish", "FINISHED", "finish", model.PlayEvent{}, 0, "FINISHED"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			memory := newPlayMemory()
			memory.source.Ref.Preview = true
			memory.source.Session.State = test.state
			result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", test.kind, test.event)
			if err != nil || result.PlaySessionID != nil || result.AcceptedDuration != 0 || result.State != test.want ||
				result.ClientSequence != test.event.ClientSequence || memory.reads != 1 || memory.writes != test.writes {
				t.Fatalf("preview result=%#v reads=%d writes=%d error=%v", result, memory.reads, memory.writes, err)
			}
			if memory.start.PlayID != "" || memory.progress.Current.ID != "" {
				t.Fatal("preview entered product writer")
			}
		})
	}
}

func TestProductLoadingFinishIsIdempotentAndHasNoTime(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED"} {
		memory := newPlayMemory()
		memory.source.Session.State = state
		result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "finish", model.PlayEvent{})
		writes := 1
		if state == "FINISHED" {
			writes = 0
		}
		if err != nil || result.State != "FINISHED" || result.PlaySessionID != nil || result.AcceptedDuration != 0 || memory.writes != writes {
			t.Fatalf("state=%s result=%#v writes=%d error=%v", state, result, memory.writes, err)
		}
	}
}
