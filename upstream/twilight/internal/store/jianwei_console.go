package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type ConsoleState struct {
	Drafts map[string]ConsoleDraft `json:"drafts"`
}
type ConsoleDraft struct {
	ID        string         `json:"id"`
	Module    string         `json:"module"`
	Revision  int64          `json:"revision"`
	Status    string         `json:"status"`
	Expected  string         `json:"expected"`
	Applied   string         `json:"applied"`
	Sections  map[string]any `json:"sections"`
	Before    map[string]any `json:"before"`
	Reason    string         `json:"reason"`
	Error     string         `json:"error,omitempty"`
	Actor     int64          `json:"actor"`
	At        int64          `json:"at"`
	UpdatedAt int64          `json:"updated_at"`
}

func (s *Store) ConsoleDrafts() []ConsoleDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ConsoleDraft{}
	for _, d := range s.state.Console.Drafts {
		b, _ := json.Marshal(d)
		var copy ConsoleDraft
		_ = json.Unmarshal(b, &copy)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}
func (s *Store) ConsoleDraft(id string) (ConsoleDraft, bool) {
	for _, v := range s.ConsoleDrafts() {
		if v.ID == id {
			return v, true
		}
	}
	return ConsoleDraft{}, false
}
func (s *Store) SaveConsoleDraft(d ConsoleDraft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		if s.state.Console.Drafts == nil {
			s.state.Console.Drafts = map[string]ConsoleDraft{}
		}
		if _, ok := s.state.Console.Drafts[d.ID]; ok {
			return ErrConflict
		}
		if len(s.state.Console.Drafts) >= 256 {
			return ErrConflict
		}
		d.Status = "draft"
		d.Revision = 1
		d.At = time.Now().Unix()
		d.UpdatedAt = d.At
		s.state.Console.Drafts[d.ID] = d
		return nil
	})
}

func ValidateConsoleBackup(state *State) error {
	if len(state.Console.Drafts) > 256 {
		return fmt.Errorf("too many control drafts")
	}
	for id, d := range state.Console.Drafts {
		if id != d.ID || d.Revision < 1 || len(d.Expected) != 64 || len(d.Sections) > 12 || len(d.Reason) > 500 {
			return fmt.Errorf("invalid control draft")
		}
		data, _ := json.Marshal(d)
		if len(data) > 131072 {
			return fmt.Errorf("control draft too large")
		}
		switch d.Status {
		case "draft", "discarded", "applying", "applied", "overridden", "rolled_back", "failed":
		default:
			return fmt.Errorf("invalid control status")
		}
		if d.Status == "applying" {
			d.Status = "failed"
			d.Error = "导入时原执行上下文失效，请核对配置后重新预览"
			d.Revision++
			state.Console.Drafts[id] = d
		}
	}
	return nil
}
func (s *Store) TransitionConsoleDraft(id string, revision int64, from, to, applied, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		d, ok := s.state.Console.Drafts[id]
		if !ok {
			return ErrNotFound
		}
		if d.Revision != revision || d.Status != from {
			return ErrConflict
		}
		d.Status = to
		d.UpdatedAt = time.Now().Unix()
		d.Revision++
		d.Applied = applied
		d.Error = message
		s.state.Console.Drafts[id] = d
		return nil
	})
}
