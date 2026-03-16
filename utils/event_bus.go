package utils

import "sync"

type StreamEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Stage   string `json:"stage,omitempty"`
	ZipFile string `json:"zip_file,omitempty"`
	JobID   string `json:"job_id,omitempty"`
}

type EventBus struct {
	mu     sync.RWMutex
	subs   map[int]chan StreamEvent
	nextID int
}

var defaultEventBus = &EventBus{
	subs: make(map[int]chan StreamEvent),
}

func SubscribeEvents(buffer int) (int, <-chan StreamEvent, func()) {
	return defaultEventBus.Subscribe(buffer)
}

func PublishEvent(event StreamEvent) {
	defaultEventBus.Publish(event)
}

func (b *EventBus) Subscribe(buffer int) (int, <-chan StreamEvent, func()) {
	if buffer <= 0 {
		buffer = 128
	}

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan StreamEvent, buffer)
	b.subs[id] = ch
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}

	return id, ch, cancel
}

func (b *EventBus) Publish(event StreamEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
