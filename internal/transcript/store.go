package transcript

import (
	"encoding/json"
	"os"

	"github.com/mossagi/moss/internal/events"
)

type TranscriptStore struct {
	file *os.File
	enc  *json.Encoder
}

func NewTranscriptStore(path string) (*TranscriptStore, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &TranscriptStore{file: f, enc: json.NewEncoder(f)}, nil
}

func (s *TranscriptStore) Write(event events.Event) error {
	return s.enc.Encode(event)
}

func (s *TranscriptStore) Close() error {
	return s.file.Close()
}
