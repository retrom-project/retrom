package netplay

import model "retrom/internal/model/netplay"

func stateEvent(kind, from, to, reason string) model.SessionEvent {
	return model.SessionEvent{
		Type: kind,
		Data: model.SessionEventData{SchemaVersion: 1, FromState: from, ToState: to, Reason: reason},
	}
}
