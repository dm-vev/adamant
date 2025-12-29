package chat

import (
	"github.com/google/uuid"
	"sync"
)

// Global represents a global chat. Players will write in this chat by default
// when they send any message in the chat.
var Global = New()

// Chat represents the in-game chat. Messages may be written to it to send a
// message to all subscribers.
// Methods on Chat may be called from multiple goroutines concurrently. Chat
// implements the io.Writer and io.StringWriter interfaces. fmt.Fprintf and
// fmt.Fprint may be used to write formatted messages to the chat.
type Chat struct {
	m           sync.Mutex
	subscribers map[uuid.UUID]Subscriber
}

// New returns a new chat.
func New() *Chat {
	return &Chat{subscribers: map[uuid.UUID]Subscriber{}}
}

// Write writes the byte slice p as a string to the chat. It is equivalent to
// calling Chat.WriteString(string(p)).
func (chat *Chat) Write(p []byte) (n int, err error) {
	return chat.WriteString(string(p))
}

// WriteString writes a string s to the chat.
func (chat *Chat) WriteString(s string) (n int, err error) {
	subscribers := chat.snapshotSubscribers()
	for _, subscriber := range subscribers {
		subscriber.Message(s)
	}
	return len(s), nil
}

// Writet writes a Translation message to a Chat, parameterising the message
// using the arguments passed. Messages are translated according to the locale
// of subscribers if they implement Translator. Subscribers that do not
// implement Translator have the fallback message sent.
func (chat *Chat) Writet(t Translation, a ...any) {
	subscribers := chat.snapshotSubscribers()
	for _, subscriber := range subscribers {
		if translator, ok := subscriber.(Translator); ok {
			translator.Messaget(t, a...)
			continue
		}
		subscriber.Message(t.F(a...).String())
	}
}

// Subscribe adds a subscriber to the chat, sending it every message written to
// the chat. In order to remove it again, use Chat.Unsubscribe().
func (chat *Chat) Subscribe(s Subscriber) {
	chat.m.Lock()
	defer chat.m.Unlock()
	if chat.subscribers == nil {
		// Chat was closed.
		return
	}
	chat.subscribers[s.UUID()] = s
}

// Subscribed checks if a subscriber is currently subscribed to the chat.
func (chat *Chat) Subscribed(s Subscriber) bool {
	chat.m.Lock()
	defer chat.m.Unlock()
	if chat.subscribers == nil {
		return false
	}
	_, ok := chat.subscribers[s.UUID()]
	return ok
}

// Unsubscribe removes a subscriber from the chat, so that messages written to
// the chat will no longer be sent to it.
func (chat *Chat) Unsubscribe(s Subscriber) {
	chat.m.Lock()
	defer chat.m.Unlock()
	if chat.subscribers == nil {
		// Chat was closed.
		return
	}
	delete(chat.subscribers, s.UUID())
}

// Close closes the chat, removing all subscribers from it.
func (chat *Chat) Close() error {
	chat.m.Lock()
	chat.subscribers = nil
	chat.m.Unlock()
	return nil
}

// snapshotSubscribers copies the current subscribers so callbacks are invoked without holding the chat lock.
func (chat *Chat) snapshotSubscribers() []Subscriber {
	chat.m.Lock()
	defer chat.m.Unlock()
	if len(chat.subscribers) == 0 {
		return nil
	}
	subscribers := make([]Subscriber, 0, len(chat.subscribers))
	for _, subscriber := range chat.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return subscribers
}
