package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type sseEvent struct {
	Type string
	Data []byte
}

type eventHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan sseEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[string]map[chan sseEvent]struct{}{}}
}

func (h *eventHub) subscribe(userID string) (chan sseEvent, func()) {
	ch := make(chan sseEvent, 32)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan sseEvent]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
		h.mu.Unlock()
	}
}

func (h *eventHub) publish(userIDs []string, event sseEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, userID := range userIDs {
		for ch := range h.subs[userID] {
			select {
			case ch <- event:
			default:
				// A slow browser must never block an outbox transaction.
			}
		}
	}
}

// RunOutboxDispatcher is the missing consumer for outbox_events. It publishes
// to in-process SSE subscribers and marks events published; the table remains
// the durable audit trail for a future multi-instance broker.
func (s *Server) RunOutboxDispatcher(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	s.dispatchOutbox(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchOutbox(ctx)
		}
	}
}

func (s *Server) dispatchOutbox(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `SELECT HEX(id),COALESCE(HEX(team_id),''),event_type,payload
		FROM outbox_events WHERE published_at IS NULL AND attempts<5 ORDER BY created_at LIMIT 50`)
	if err != nil {
		log.Printf("outbox dispatch query failed: %v", err)
		return
	}
	type eventRow struct {
		id, teamID, eventType string
		payload               []byte
	}
	events := make([]eventRow, 0)
	for rows.Next() {
		var event eventRow
		if err = rows.Scan(&event.id, &event.teamID, &event.eventType, &event.payload); err != nil {
			rows.Close()
			log.Printf("outbox dispatch scan failed: %v", err)
			return
		}
		events = append(events, event)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		log.Printf("outbox dispatch rows failed: %v", err)
		return
	}
	for _, event := range events {
		res, err := s.DB.ExecContext(ctx, `UPDATE outbox_events SET attempts=attempts+1 WHERE id=UNHEX(?) AND published_at IS NULL`, event.id)
		if err != nil {
			continue
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			continue
		}
		recipients := s.outboxRecipients(ctx, event.teamID, event.payload)
		s.events.publish(recipients, sseEvent{Type: event.eventType, Data: event.payload})
		if _, err = s.DB.ExecContext(ctx, `UPDATE outbox_events SET published_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, event.id); err != nil {
			log.Printf("outbox mark published failed id=%s: %v", event.id, err)
		}
	}
}

func (s *Server) outboxRecipients(ctx context.Context, teamID string, payload []byte) []string {
	var body struct {
		AudienceUserID string `json:"audienceUserId"`
	}
	_ = json.Unmarshal(payload, &body)
	if body.AudienceUserID != "" {
		return []string{body.AudienceUserID}
	}
	if teamID == "" {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT HEX(user_id) FROM team_memberships WHERE team_id=UNHEX(?) AND status='active'`, teamID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	recipients := make([]string, 0)
	for rows.Next() {
		var userID string
		if rows.Scan(&userID) == nil {
			recipients = append(recipients, userID)
		}
	}
	return recipients
}

func (s *Server) eventsStream(w http.ResponseWriter, r *http.Request, u User) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// The API server sets a bounded WriteTimeout for normal requests. SSE is a
	// long-lived response, so clear the deadline for this connection only.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	events, unsubscribe := s.events.subscribe(u.ID)
	defer unsubscribe()
	_, _ = fmt.Fprint(w, "event: ready\ndata: {\"connected\":true}\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case event := <-events:
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
