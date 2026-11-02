package live

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReminderPhraseParsing(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 20, 18, 0, 0, 0, loc)
	if title := reminderTitle("我明天要干一个活，就是建立一个官网那个"); title != "建立一个官网" {
		t.Fatalf("unexpected title %q", title)
	}
	if title := reminderTitle("记到我的日程上。"); title != "" {
		t.Fatalf("action phrase should not become a title: %q", title)
	}
	if day, ok := parseReminderDay("明天上午十点", now); !ok || day.Day() != 21 {
		t.Fatalf("tomorrow was not parsed: %v %v", day, ok)
	}
	if hour, minute, ok := parseReminderClock("明天上午十点半"); !ok || hour != 10 || minute != 30 {
		t.Fatalf("Chinese time was not parsed: %d:%d %v", hour, minute, ok)
	}
	if hour, minute, ok := parseReminderClock("明天下午3点30分"); !ok || hour != 15 || minute != 30 {
		t.Fatalf("afternoon time was not parsed: %d:%d %v", hour, minute, ok)
	}
}

func TestReminderDateAndClockMatrix(t *testing.T) {
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, shanghai) // Tuesday
	for _, item := range []struct {
		input        string
		hour, minute int
		valid        bool
	}{
		{"上午十点", 10, 0, true}, {"十点一刻", 10, 15, true}, {"下午三点半", 15, 30, true},
		{"晚上八点三刻", 20, 45, true}, {"上午十点零五分", 10, 5, true}, {"凌晨十二点", 0, 0, true},
		{"23:05", 23, 5, true}, {"下午3:30", 15, 30, true}, {"25点", 0, 0, false},
		{"10点90分", 0, 0, false}, {"10:99", 0, 0, false}, {"十点九十", 0, 0, false},
	} {
		hour, minute, valid := parseReminderClock(item.input)
		if valid != item.valid || valid && (hour != item.hour || minute != item.minute) {
			t.Errorf("clock %q = %02d:%02d valid=%v", item.input, hour, minute, valid)
		}
	}
	for _, item := range []struct {
		input string
		day   int
		valid bool
	}{
		{"今天", 20, true}, {"明早", 21, true}, {"后天", 22, true}, {"本周五", 23, true},
		{"下周一", 26, true}, {"周一", 26, true}, {"2026-10-24", 24, true},
		{"10月24日", 24, true}, {"2026-02-30", 0, false}, {"2月30日", 0, false},
	} {
		day, valid := parseReminderDay(item.input, now)
		if valid != item.valid || valid && day.Day() != item.day {
			t.Errorf("day %q = %v valid=%v", item.input, day, valid)
		}
	}
	for _, item := range []struct{ input, want string }{
		{"明天上午十点提醒我开会", "开会"},
		{"下周一上午十点把建立官网记到我的日程上", "建立官网"},
		{"记到我的日程上。", ""},
	} {
		if got := reminderTitle(item.input); got != item.want {
			t.Errorf("title %q = %q, want %q", item.input, got, item.want)
		}
	}
}

func TestAssistantReminderFollowupPersistsOnce(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("MySQL integration test requires MYSQL_DSN")
	}
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	uid, cid, bearer := id(), id(), token()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash,timezone) VALUES(UNHEX(?),?,?,?,'Asia/Shanghai')`, uid, "assistant-reminder-"+uid+"@example.invalid", "Reminder Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.DB.Exec(`DELETE FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, uid)
		s.DB.Exec(`DELETE FROM conversation_messages WHERE conversation_id=UNHEX(?)`, cid)
		s.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, cid)
		s.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, uid)
		s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, uid)
	}()
	if _, err = s.DB.Exec(`INSERT INTO conversations(id,owner_user_id) VALUES(UNHEX(?),UNHEX(?))`, cid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,UTC_TIMESTAMP(3)+INTERVAL 1 HOUR)`, id(), uid, hash(bearer)); err != nil {
		t.Fatal(err)
	}
	// This is the earlier WeChat utterance. The action itself is sent through
	// the same browser conversation API to check the shared planning path.
	if _, err = s.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user',?)`, id(), cid, "我明天要干一个活，就是建立一个官网那个"); err != nil {
		t.Fatal(err)
	}
	send := func(content, endpoint string) (int, string) {
		t.Helper()
		input, _ := json.Marshal(map[string]any{"content": content})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/me/conversations/"+cid+"/"+endpoint, bytes.NewReader(input))
		req.Header.Set("Authorization", "Bearer "+bearer)
		response := httptest.NewRecorder()
		s.ServeHTTP(response, req)
		return response.Code, response.Body.String()
	}
	status, response := send("记到我的日程上。", "messages")
	if status != http.StatusCreated || !strings.Contains(response, "请告诉我具体日期和时间") || strings.Contains(response, "已为你创建") {
		t.Fatalf("missing-time follow-up status=%d response=%s", status, response)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reminder created without a time: count=%d err=%v", count, err)
	}
	status, response = send("明天上午十点", "stream")
	if status != http.StatusOK || !strings.Contains(response, "已为你创建定时提醒：建立一个官网") || !strings.Contains(response, "event: done") {
		t.Fatalf("completion status=%d response=%s", status, response)
	}
	var title string
	var runAt time.Time
	if err = s.DB.QueryRow(`SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.title')),run_at FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, uid).Scan(&title, &runAt); err != nil || title != "建立一个官网" {
		t.Fatalf("saved reminder title=%q at=%v err=%v", title, runAt, err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	if runAt.In(loc).Hour() != 10 || runAt.In(loc).Day() != time.Now().In(loc).AddDate(0, 0, 1).Day() {
		t.Fatalf("saved reminder has wrong local time: %v", runAt.In(loc))
	}
	var sourceMessageID string
	if err = s.DB.QueryRow(`SELECT HEX(id) FROM conversation_messages WHERE conversation_id=UNHEX(?) AND role='user' AND content='明天上午十点'`, cid).Scan(&sourceMessageID); err != nil {
		t.Fatal(err)
	}
	plan := assistantReminder{Title: title, RunAt: runAt}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.insertAssistantReminder(context.Background(), tx, uid, sourceMessageID, plan); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, uid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry duplicated reminder: count=%d err=%v", count, err)
	}
	if _, err = s.DB.Exec(`UPDATE users SET status='disabled' WHERE id=UNHEX(?)`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.savePlannedAssistantReminder(context.Background(), cid, uid, "后天上午十点提醒我检查官网", assistantReminder{Handled: true, Title: "检查官网", RunAt: time.Now().Add(48 * time.Hour), Reply: "已为你创建定时提醒"}); err == nil {
		t.Fatal("disabled user was falsely told a reminder was saved")
	}
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM conversation_messages WHERE conversation_id=UNHEX(?) AND content='已为你创建定时提醒'`, cid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed write left a success reply: count=%d err=%v", count, err)
	}
}

func TestLegacyPromiseCanBeCompletedWithoutRepeatingIt(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("MySQL integration test requires MYSQL_DSN")
	}
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	uid, cid := id(), id()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash,timezone) VALUES(UNHEX(?),?,?,?,'Asia/Shanghai')`, uid, "legacy-reminder-"+uid+"@example.invalid", "Reminder Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.DB.Exec(`DELETE FROM conversation_messages WHERE conversation_id=UNHEX(?)`, cid)
		s.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, cid)
		s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, uid)
	}()
	if _, err = s.DB.Exec(`INSERT INTO conversations(id,owner_user_id) VALUES(UNHEX(?),UNHEX(?))`, cid, uid); err != nil {
		t.Fatal(err)
	}
	for index, item := range []struct{ role, content string }{
		{"user", "我明天要干一个活，就是建立一个官网那个"},
		{"user", "记到我的日程上。"},
		{"assistant", "好的，我会将建立官方网站的任务记录在您的日程中。"},
	} {
		if _, err = s.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content,created_at) VALUES(UNHEX(?),UNHEX(?),?,?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL ? SECOND))`, id(), cid, item.role, item.content, index); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := s.planAssistantReminder(context.Background(), cid, "", "明天上午十点", "Asia/Shanghai", time.Now())
	if err != nil || !plan.Handled || plan.Title != "建立一个官网" || plan.RunAt.IsZero() {
		t.Fatalf("legacy promise was not recoverable: %+v err=%v", plan, err)
	}
	for _, item := range []struct {
		prompt, title      string
		scheduled, handled bool
	}{
		{"明天上午十点提醒我给我妈打电话", "给我妈打电话", true, true},
		{"下周五下午三点把发布公告提上日程", "发布公告", true, true},
		{"2026-02-30上午十点提醒我开会", "开会", false, true},
		{"明天25点提醒我开会", "开会", false, true},
		{"明天10点90分提醒我开会", "开会", false, true},
		{"别提醒我明天十点开会", "", false, false},
	} {
		planned, planErr := s.planAssistantReminder(context.Background(), cid, "", item.prompt, "Asia/Shanghai", time.Now())
		if planErr != nil || planned.Handled != item.handled || planned.Title != item.title || !planned.RunAt.IsZero() != item.scheduled {
			t.Errorf("prompt %q planned %+v err=%v", item.prompt, planned, planErr)
		}
	}
	newYork, _ := time.LoadLocation("America/New_York")
	dstNow := time.Date(2026, 3, 7, 12, 0, 0, 0, newYork)
	dst, err := s.planAssistantReminder(context.Background(), cid, "", "2026-03-08凌晨2点30分提醒我开会", "America/New_York", dstNow)
	if err != nil || !dst.Handled || !dst.RunAt.IsZero() || !strings.Contains(dst.Reply, "时间在你的时区不存在") {
		t.Fatalf("DST gap must ask for another time: %+v err=%v", dst, err)
	}
}

func TestReminderTitleHandlesLeadingAndTrailingTriggers(t *testing.T) {
	cases := []struct{ input, want string }{
		{"提醒我明天上午10点建立官网", "建立官网"},
		{"我今天下午3点的时候有一个会议，到时候提醒我", "会议"},
		{"帮我设置提醒，明天下午3点开会", "开会"},
		{"明天下午三点提醒我交周报", "交周报"},
		{"提醒我一下，明天下午三点开会", "开会"},
		{"我今天下午3点有一个会议，提醒我一下", "会议"},
		{"改为一个小时后", ""},
		{"改为明天15:00", ""},
	}
	for _, item := range cases {
		if got := reminderTitle(item.input); got != item.want {
			t.Errorf("reminderTitle(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}

func TestParseReminderRelativeDurations(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 33, 0, 0, time.FixedZone("CST", 8*3600))
	cases := []struct {
		input string
		want  time.Time
	}{
		{"改为一个小时后", now.Add(time.Hour)},
		{"半小时后", now.Add(30 * time.Minute)},
		{"一个半小时后", now.Add(90 * time.Minute)},
		{"30分钟后", now.Add(30 * time.Minute)},
		{"两小时以后", now.Add(2 * time.Hour)},
	}
	for _, item := range cases {
		got, ok := parseReminderRelative(item.input, now)
		if !ok || !got.Equal(item.want) {
			t.Errorf("parseReminderRelative(%q) = %s, %v; want %s", item.input, got, ok, item.want)
		}
	}
	if got := reminderTitle("提醒我一小时后开会"); got != "开会" {
		t.Errorf("relative title = %q, want 开会", got)
	}
}
