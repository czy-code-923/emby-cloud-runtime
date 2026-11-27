package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrMediaRequestProgressManaged = errors.New("media request progress is managed by its worker")

func manualMediaRequestStatusAllowed(request MediaRequest, status string) bool {
	return request.Tracking.JobID == "" || (status != MediaRequestStatusDownloading && status != MediaRequestStatusCompleted)
}

type MediaRequestEvent struct {
	ID       string `json:"id,omitempty"`
	Digest   string `json:"digest,omitempty"`
	Stage    string `json:"stage"`
	Progress int    `json:"progress"`
	Note     string `json:"note,omitempty"`
	At       int64  `json:"at"`
}
type MediaRequestTracking struct {
	JobID       string              `json:"job_id,omitempty"`
	Sequence    int64               `json:"sequence,omitempty"`
	Stage       string              `json:"stage,omitempty"`
	Progress    int                 `json:"progress,omitempty"`
	MediaItemID string              `json:"media_item_id,omitempty"`
	Events      []MediaRequestEvent `json:"events,omitempty"`
}
type MediaRequestWorkerUpdate struct {
	JobID       string `json:"job_id"`
	EventID     string `json:"event_id"`
	Sequence    int64  `json:"sequence"`
	Stage       string `json:"stage"`
	Progress    int    `json:"progress"`
	Note        string `json:"note"`
	MediaItemID string `json:"media_item_id"`
}

func MediaRequestWorkerDigest(update MediaRequestWorkerUpdate) string {
	raw, _ := json.Marshal(update)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func MediaRequestWorkerStage(stage string) (string, int) {
	switch stage {
	case "queued":
		return MediaRequestStatusAccepted, 1
	case "searching":
		return MediaRequestStatusAccepted, 2
	case "downloading":
		return MediaRequestStatusDownloading, 3
	case "importing":
		return MediaRequestStatusDownloading, 4
	case "completed":
		return MediaRequestStatusCompleted, 5
	case "failed":
		return MediaRequestStatusFailed, 6
	}
	return "", 0
}

// The callback updates the same canonical request, under the existing JSONB
// CAS. Event replay is idempotent; stale jobs and out-of-order events cannot
// move a request backwards or bypass an administrator's rejection.
func (s *Store) UpdateMediaRequestFromWorker(key string, update MediaRequestWorkerUpdate, expected int64) (MediaRequest, error) {
	status, rank := MediaRequestWorkerStage(update.Stage)
	if strings.TrimSpace(key) == "" || status == "" || update.Sequence < 1 || len(update.JobID) < 1 || len(update.JobID) > 100 || len(update.EventID) < 8 || len(update.EventID) > 100 || len(update.Note) > 3000 || update.Progress < 0 || update.Progress > 100 || expected < 0 || len(update.MediaItemID) > 100 {
		return MediaRequest{}, ErrInvalid
	}
	digest := MediaRequestWorkerDigest(update)
	s.mu.Lock()
	defer s.mu.Unlock()
	var result MediaRequest
	err := s.mutateAndSaveLocked(func() error {
		result = MediaRequest{}
		id, r, found := s.mediaRequestByKeyLocked(key)
		if !found {
			return ErrNotFound
		}
		for _, event := range r.Tracking.Events {
			if event.ID == update.EventID {
				if event.Digest != digest {
					return ErrConflict
				}
				result = r
				return errNoChange
			}
		}
		if r.Revision != expected || r.Status == MediaRequestStatusUnhandled || r.Status == MediaRequestStatusRejected || r.Status == MediaRequestStatusCompleted || r.Status == MediaRequestStatusFailed {
			return ErrConflict
		}
		old := r.Tracking
		if (old.JobID != "" && old.JobID != update.JobID) || update.Sequence <= old.Sequence {
			return ErrConflict
		}
		_, previousRank := MediaRequestWorkerStage(old.Stage)
		if rank < previousRank || (old.Stage == update.Stage && update.Progress < old.Progress) {
			return ErrConflict
		}
		applyMediaRequestStatusUpdate(&r, status, update.Note, true)
		r.Tracking.JobID = update.JobID
		r.Tracking.Sequence = update.Sequence
		r.Tracking.Stage = update.Stage
		r.Tracking.Progress = update.Progress
		r.Tracking.MediaItemID = update.MediaItemID
		events := cloneSlice(old.Events)
		events = append(events, MediaRequestEvent{ID: update.EventID, Digest: digest, Stage: update.Stage, Progress: update.Progress, Note: update.Note, At: r.UpdatedAt})
		if len(events) > 20 {
			events = cloneSlice(events[len(events)-20:])
		}
		r.Tracking.Events = events
		s.state.MediaRequests[id] = r
		result = r
		return nil
	})
	if err != nil {
		return MediaRequest{}, err
	}
	return cloneMediaRequest(result), nil
}

func (s *Store) MediaWorkerHealth() (lastSeen int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.MediaWorkerLastSeen
}
func (s *Store) TouchMediaWorker(now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		if now-s.state.MediaWorkerLastSeen < 30 {
			return errNoChange
		}
		s.state.MediaWorkerLastSeen = now
		return nil
	})
}

func (s *Store) ClaimMediaRequestNotice() (MediaRequest, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result MediaRequest
	var telegramID int64
	found := false
	now := time.Now().Unix()
	err := s.mutateAndSaveLocked(func() error {
		result = MediaRequest{}
		telegramID = 0
		found = false
		for id, r := range s.state.MediaRequests {
			if !r.NotificationPending || r.NotificationLeaseUntil > now {
				continue
			}
			u, ok := s.state.Users[r.UID]
			if !ok || !u.Active || u.TelegramID <= 0 {
				continue
			}
			r.NotificationLeaseUntil = now + 120
			s.state.MediaRequests[id] = r
			result = r
			telegramID = u.TelegramID
			found = true
			return nil
		}
		return errNoChange
	})
	return cloneMediaRequest(result), telegramID, found, err
}
func (s *Store) MarkMediaRequestNotice(id, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		r, ok := s.state.MediaRequests[id]
		if !ok {
			return ErrNotFound
		}
		if r.Revision != revision {
			return ErrConflict
		}
		r.NotificationPending = false
		r.NotificationLeaseUntil = 0
		s.state.MediaRequests[id] = r
		return nil
	})
}

func ValidateMediaRequestTrackingBackup(state *State) error {
	for _, request := range state.MediaRequests {
		v := request.Tracking
		if len(v.Events) > 20 || len(v.JobID) > 100 || v.Sequence < 0 || v.Progress < 0 || v.Progress > 100 || len(v.MediaItemID) > 100 {
			return fmt.Errorf("invalid media tracking backup")
		}
		if v.Stage != "" {
			if _, rank := MediaRequestWorkerStage(v.Stage); rank == 0 {
				return fmt.Errorf("invalid media stage backup")
			}
		}
		for _, e := range v.Events {
			if len(e.ID) > 100 || len(e.Digest) != 64 || e.Progress < 0 || e.Progress > 100 || len(e.Note) > 3000 || e.At < 0 {
				return fmt.Errorf("invalid media event backup")
			}
			if _, rank := MediaRequestWorkerStage(e.Stage); rank == 0 {
				return fmt.Errorf("invalid media event stage")
			}
		}
	}
	for _, user := range state.Users {
		v := user.MemberSecurityAttempts
		if v.Failures < 0 || v.Failures > 5 || v.WindowStart < 0 || v.LockedUntil < 0 || (v.LockedUntil > 0 && v.LockedUntil-v.WindowStart > 600) {
			return fmt.Errorf("invalid member proof backup")
		}
	}
	// Host liveness and delivery leases are operational, not imported facts.
	state.MediaWorkerLastSeen = 0
	state.MediaClientLogins = nil
	for id, request := range state.MediaRequests {
		request.NotificationLeaseUntil = 0
		state.MediaRequests[id] = request
	}
	return nil
}
