package live

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestReminderAPIAndWorkerArePersistentAndIdempotent(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("MySQL integration test requires MYSQL_DSN")
	}
	s, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires the migrated schema: %v", err)
	}
	defer s.DB.Close()
	ownerID, otherID := id(), id()
	ownerToken, otherToken := token(), token()
	for _, fixture := range []struct{ uid, email, bearer string }{
		{ownerID, "reminder-" + ownerID + "@example.invalid", ownerToken},
		{otherID, "reminder-" + otherID + "@example.invalid", otherToken},
	} {
		if _, err := s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, fixture.uid, fixture.email, "Reminder Test", "not-a-login"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,UTC_TIMESTAMP(3)+INTERVAL 1 HOUR)`, id(), fixture.uid, hash(fixture.bearer)); err != nil {
			t.Fatal(err)
		}
	}
	teamID := id()
	if _, err := s.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Reminder Team',?,'active',UNHEX(?),JSON_OBJECT())`, teamID, "reminder-"+teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		s.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		for _, uid := range []string{ownerID, otherID} {
			s.DB.Exec(`DELETE d FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.recipient_user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM notifications WHERE recipient_user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM notification_preferences WHERE user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM team_memberships WHERE user_id=UNHEX(?)`, uid)
			s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, uid)
		}
	}()

	input := map[string]any{"title": "reminder integration", "body": "persist and deliver", "runAt": time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)}
	encoded, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/reminders", bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("create reminder status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || !validID(envelope.Data.ID) {
		t.Fatalf("invalid create response: %s err=%v", response.Body.String(), err)
	}
	jobID := envelope.Data.ID
	for _, invalidTime := range []string{
		time.Now().Add(time.Hour).Format("2006-01-02T15:04:05"),
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	} {
		invalidBody, _ := json.Marshal(map[string]any{"title": "invalid time", "runAt": invalidTime})
		invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/reminders", bytes.NewReader(invalidBody))
		invalidRequest.Header.Set("Authorization", "Bearer "+ownerToken)
		invalidResponse := httptest.NewRecorder()
		s.ServeHTTP(invalidResponse, invalidRequest)
		if invalidResponse.Code != http.StatusBadRequest {
			t.Fatalf("invalid reminder time %q status=%d body=%s", invalidTime, invalidResponse.Code, invalidResponse.Body.String())
		}
	}

	teamRequestBody, _ := json.Marshal(map[string]any{"teamId": teamID, "title": "team reminder", "runAt": time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)})
	nonMemberRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/reminders", bytes.NewReader(teamRequestBody))
	nonMemberRequest.Header.Set("Authorization", "Bearer "+otherToken)
	nonMemberResponse := httptest.NewRecorder()
	s.ServeHTTP(nonMemberResponse, nonMemberRequest)
	if nonMemberResponse.Code != http.StatusForbidden {
		t.Fatalf("non-member team reminder status=%d body=%s", nonMemberResponse.Code, nonMemberResponse.Body.String())
	}
	teamCreate := httptest.NewRecorder()
	teamRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/reminders", bytes.NewReader(teamRequestBody))
	teamRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	s.ServeHTTP(teamCreate, teamRequest)
	if teamCreate.Code != http.StatusCreated {
		t.Fatalf("team member create reminder status=%d body=%s", teamCreate.Code, teamCreate.Body.String())
	}
	var teamEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(teamCreate.Body.Bytes(), &teamEnvelope); err != nil || !validID(teamEnvelope.Data.ID) {
		t.Fatalf("invalid team reminder response %s: %v", teamCreate.Body.String(), err)
	}
	teamCancel := httptest.NewRecorder()
	teamCancelRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/me/reminders/"+teamEnvelope.Data.ID, nil)
	teamCancelRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	s.ServeHTTP(teamCancel, teamCancelRequest)
	if teamCancel.Code != http.StatusOK {
		t.Fatalf("team reminder cancel status=%d body=%s", teamCancel.Code, teamCancel.Body.String())
	}

	secondInput, _ := json.Marshal(map[string]any{"title": "cancelled reminder", "runAt": time.Now().Add(3 * time.Minute).UTC().Format(time.RFC3339)})
	secondCreate := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/reminders", bytes.NewReader(secondInput))
	secondRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	s.ServeHTTP(secondCreate, secondRequest)
	if secondCreate.Code != http.StatusCreated {
		t.Fatalf("second reminder status=%d body=%s", secondCreate.Code, secondCreate.Body.String())
	}
	var secondEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(secondCreate.Body.Bytes(), &secondEnvelope); err != nil || !validID(secondEnvelope.Data.ID) {
		t.Fatalf("invalid second response %s: %v", secondCreate.Body.String(), err)
	}
	cancel := httptest.NewRecorder()
	cancelRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/me/reminders/"+secondEnvelope.Data.ID, nil)
	cancelRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	s.ServeHTTP(cancel, cancelRequest)
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
	var cancelled string
	if err := s.DB.QueryRow(`SELECT status FROM scheduled_jobs WHERE id=UNHEX(?)`, secondEnvelope.Data.ID).Scan(&cancelled); err != nil || cancelled != "cancelled" {
		t.Fatalf("cancelled job status=%s err=%v", cancelled, err)
	}
	if _, err := s.DB.Exec(`UPDATE scheduled_jobs SET run_at=UTC_TIMESTAMP(3)-INTERVAL 1 SECOND WHERE id=UNHEX(?)`, jobID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.processDueReminders(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var status string
	var attempts int
	if err := s.DB.QueryRow(`SELECT status,attempts FROM scheduled_jobs WHERE id=UNHEX(?)`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || attempts != 1 {
		t.Fatalf("job status=%s attempts=%d", status, attempts)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?) AND title='reminder integration'`, ownerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("notification count=%d; want exactly once", count)
	}
	if err := s.processDueReminders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?) AND title='reminder integration'`, ownerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("replay notification count=%d; want exactly once", count)
	}

	for _, test := range []struct {
		bearer string
		want   int
	}{{ownerToken, http.StatusOK}, {otherToken, http.StatusOK}} {
		list := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/me/reminders", nil)
		request.Header.Set("Authorization", "Bearer "+test.bearer)
		s.ServeHTTP(list, request)
		if list.Code != test.want {
			t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
		}
		var result struct {
			Data []reminder `json:"data"`
		}
		if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		for _, item := range result.Data {
			if test.bearer == otherToken && item.ID == jobID {
				t.Fatal("another account can read a private reminder")
			}
		}
		if test.bearer == ownerToken && len(result.Data) != 3 {
			t.Fatalf("owner sees %d reminders", len(result.Data))
		}
	}
}
