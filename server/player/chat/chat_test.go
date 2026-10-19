package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSubscriberIdentityCanReenterChat(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name    string
		prepare bool
		call    func(*Chat, Subscriber) bool
		want    bool
	}{
		{
			name: "subscribe",
			call: func(chat *Chat, subscriber Subscriber) bool {
				chat.Subscribe(subscriber)
				return chat.Subscribed(subscriber)
			},
			want: true,
		},
		{
			name:    "subscribed",
			prepare: true,
			call:    func(chat *Chat, subscriber Subscriber) bool { return chat.Subscribed(subscriber) },
			want:    true,
		},
		{
			name:    "unsubscribe",
			prepare: true,
			call: func(chat *Chat, subscriber Subscriber) bool {
				chat.Unsubscribe(subscriber)
				return chat.Subscribed(subscriber)
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chat := New()
			subscriber := reentrantSubscriber{chat: chat, id: id}
			if test.prepare {
				chat.subscribers[id] = testSubscriber{id: id}
			}

			done := make(chan bool, 1)
			go func() { done <- test.call(chat, subscriber) }()
			select {
			case got := <-done:
				if got != test.want {
					t.Fatalf("operation result = %t, want %t", got, test.want)
				}
			case <-time.After(time.Second):
				t.Fatal("subscriber UUID re-entry deadlocked on the chat mutex")
			}
		})
	}
}

type reentrantSubscriber struct {
	chat *Chat
	id   uuid.UUID
}

func (s reentrantSubscriber) UUID() uuid.UUID {
	// Writing re-enters the chat's subscriber snapshot lock.
	s.chat.WriteString("identity lookup")
	return s.id
}

func (reentrantSubscriber) Message(...any) {}

type testSubscriber struct {
	id uuid.UUID
}

func (s testSubscriber) UUID() uuid.UUID { return s.id }
func (testSubscriber) Message(...any)    {}
