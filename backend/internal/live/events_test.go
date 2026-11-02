package live

import (
	"testing"
	"time"
)

func TestEventHubPublishesToSubscriberOnly(t *testing.T) {
	hub := newEventHub()
	ch, unsubscribe := hub.subscribe("user-a")
	defer unsubscribe()
	other, unsubscribeOther := hub.subscribe("user-b")
	defer unsubscribeOther()
	hub.publish([]string{"user-a"}, sseEvent{Type: "notification.created", Data: []byte(`{"ok":true}`)})
	select {
	case event := <-ch:
		if event.Type != "notification.created" || string(event.Data) != `{"ok":true}` {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event")
	}
	select {
	case event := <-other:
		t.Fatalf("unrelated subscriber received event: %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}
