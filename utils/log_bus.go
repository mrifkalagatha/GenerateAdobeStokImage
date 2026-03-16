package utils

import (
	"io"
	"strings"
	"sync"
)

type LogBus struct {
	mu     sync.RWMutex
	subs   map[int]chan string
	nextID int
}

var defaultLogBus = &LogBus{
	subs: make(map[int]chan string),
}

func SubscribeLogs(buffer int) (int, <-chan string, func()) {
	return defaultLogBus.Subscribe(buffer)
}

func PublishLog(line string) {
	defaultLogBus.Publish(line)
}

func NewLogBusWriter() io.Writer {
	return &logBusWriter{}
}

func (b *LogBus) Subscribe(buffer int) (int, <-chan string, func()) {
	if buffer <= 0 {
		buffer = 128
	}

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan string, buffer)
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

func (b *LogBus) Publish(line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

type logBusWriter struct{}

func (w *logBusWriter) Write(p []byte) (int, error) {
	PublishLog(string(p))
	return len(p), nil
}

func StripANSI(s string) string {
	// Remove common ANSI escape sequences and keep original line breaks.
	replacer := strings.NewReplacer(
		"\x1b[0m", "",
		"\x1b[2m", "",
		"\x1b[31m", "",
		"\x1b[32m", "",
		"\x1b[34m", "",
		"\x1b[35m", "",
		"\x1b[36m", "",
	)
	return replacer.Replace(s)
}
