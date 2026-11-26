package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiMediaWorkerScopeApprovalOrderingAndReplay(t *testing.T) {
	a := newTestApp(t)
	admin, e := a.store().CreateUser(store.User{Username: "media-worker-admin", Role: store.RoleAdmin, Active: true})
	if e != nil {
		t.Fatal(e)
	}
	member, e := a.store().CreateUser(store.User{Username: "media-worker-member", Role: store.RoleNormal, Active: true, TelegramID: 98111})
	if e != nil {
		t.Fatal(e)
	}
	for _, fixture := range []struct {
		key         string
		uid         int64
		permissions []string
	}{{"worker-admin", admin.UID, []string{apiKeyPermissionMediaSync}}, {"worker-normal", member.UID, []string{apiKeyPermissionMediaSync}}, {"worker-ordinary-key", admin.UID, nil}} {
		if _, e = a.store().CreateAPIKey(store.APIKey{UID: fixture.uid, Name: "fixture", Hash: hashAPIKey(fixture.key), Permissions: fixture.permissions}); e != nil {
			t.Fatal(e)
		}
	}
	request, e := a.store().CreateMediaRequest(store.MediaRequest{UID: member.UID, Title: "Fixture movie", Source: "tmdb", MediaID: 99, MediaType: "movie"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, key string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	endpoint := "/api/v2/integrations/media-requests"
	for _, key := range []string{"", "worker-normal", "worker-ordinary-key"} {
		if w := call("GET", endpoint, key, nil); w.Code < 400 {
			t.Fatal("unauthorized worker", key, w.Code)
		}
	}
	event := store.MediaRequestWorkerUpdate{JobID: "fixture-job", EventID: "event-0001", Sequence: 1, Stage: "searching", Progress: 10}
	if _, e = a.store().UpdateMediaRequestFromWorker(request.RequireKey, event, request.Revision); e == nil {
		t.Fatal("unreviewed request dispatched")
	}
	request, e = a.store().UpdateMediaRequestStatusByKey(request.RequireKey, "accepted", "reviewed", false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if w := call("GET", endpoint, "worker-admin", nil); w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"telegram_id"`)) {
		t.Fatal("queue authorization/projection", w.Code, w.Body)
	}
	first, e := a.store().UpdateMediaRequestFromWorker(request.RequireKey, event, request.Revision)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := a.store().UpdateMediaRequestFromWorker(request.RequireKey, event, request.Revision)
	if e != nil || retry.Revision != first.Revision {
		t.Fatal("replay changed state", retry, e)
	}
	changed := event
	changed.Progress = 11
	if _, e = a.store().UpdateMediaRequestFromWorker(request.RequireKey, changed, first.Revision); e == nil {
		t.Fatal("event ID reused with different content")
	}
	event.EventID = "event-0002"
	event.Sequence = 2
	event.Stage = "downloading"
	event.Progress = 50
	second, e := a.store().UpdateMediaRequestFromWorker(request.RequireKey, event, first.Revision)
	if e != nil {
		t.Fatal(e)
	}
	older := event
	older.EventID = "older-event"
	older.Sequence = 1
	if _, e = a.store().UpdateMediaRequestFromWorker(request.RequireKey, older, second.Revision); e == nil {
		t.Fatal("older event moved progress backwards")
	}
	otherJob := event
	otherJob.JobID = "foreign-job"
	otherJob.EventID = "foreign-event"
	otherJob.Sequence = 3
	if _, e = a.store().UpdateMediaRequestFromWorker(request.RequireKey, otherJob, second.Revision); e == nil {
		t.Fatal("another job hijacked request")
	}
	terminal := map[string]any{"revision": second.Revision, "job_id": event.JobID, "event_id": "completed-event", "sequence": 3, "stage": "completed", "progress": 100}
	if w := call("POST", endpoint+"/"+request.RequireKey+"/events", "worker-admin", terminal); w.Code < 400 {
		t.Fatal("unverified library completion accepted")
	}
	failed := event
	failed.EventID = "failed-event"
	failed.Sequence = 3
	failed.Stage = "failed"
	failed.Note = "fixture source unavailable"
	done, e := a.store().UpdateMediaRequestFromWorker(request.RequireKey, failed, second.Revision)
	if e != nil || done.Status != store.MediaRequestStatusFailed || !done.NotificationPending {
		t.Fatal("failure was not propagated", done, e)
	}
	if _, e = a.store().UpdateMediaRequestStatusByKey(request.RequireKey, "accepted", "retry", false, nil); e != nil {
		t.Fatal("admin retry rejected", e)
	}
	fresh, _ := a.store().FindMediaRequestByKey(request.RequireKey)
	if fresh.Tracking.JobID != "" || fresh.Tracking.Sequence != 0 {
		t.Fatal("retry retained stale job")
	}
	if e = a.store().MarkMediaRequestNotice(request.ID, done.Revision); e == nil {
		t.Fatal("stale notification acknowledged a new state")
	}
	a.cfg().MediaRequestEnabled = false
	if w := call(http.MethodGet, endpoint, "worker-admin", nil); w.Code < 400 {
		t.Fatal("disabled request system still dispatched work")
	}
}

func TestJianweiMediaTrackingBackupRejectsUnboundedEvents(t *testing.T) {
	state := store.State{MediaRequests: map[int64]store.MediaRequest{1: {Tracking: store.MediaRequestTracking{Progress: 101}}}}
	if store.ValidateMediaRequestTrackingBackup(&state) == nil {
		t.Fatal("invalid progress imported")
	}
	state.MediaRequests[1] = store.MediaRequest{Tracking: store.MediaRequestTracking{Events: make([]store.MediaRequestEvent, 21)}}
	if store.ValidateMediaRequestTrackingBackup(&state) == nil {
		t.Fatal("unbounded history imported")
	}
}
