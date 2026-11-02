package live

import (
	"testing"
	"time"
)

func TestNextAIProactiveRun(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := func(value string) time.Time {
		parsed, parseErr := time.ParseInLocation("2006-01-02 15:04", value, loc)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return parsed
	}
	cases := []struct {
		name    string
		payload aiProactivePayload
		after   time.Time
		want    string
	}{
		{
			name:    "daily later today",
			payload: aiProactivePayload{Cadence: "daily", Time: "09:00", Timezone: "Asia/Shanghai"},
			after:   at("2026-10-30 08:00"),
			want:    "2026-10-30 09:00",
		},
		{
			name:    "daily already passed",
			payload: aiProactivePayload{Cadence: "daily", Time: "09:00", Timezone: "Asia/Shanghai"},
			after:   at("2026-10-30 10:00"),
			want:    "2026-10-31 09:00",
		},
		{
			name:    "weekdays skip weekend",
			payload: aiProactivePayload{Cadence: "weekdays", Time: "09:00", Timezone: "Asia/Shanghai"},
			after:   at("2026-10-30 18:00"),
			want:    "2026-11-02 09:00",
		},
		{
			name:    "weekly same day later",
			payload: aiProactivePayload{Cadence: "weekly", Time: "09:00", Weekday: 5, Timezone: "Asia/Shanghai"},
			after:   at("2026-10-30 08:00"),
			want:    "2026-10-30 09:00",
		},
		{
			name:    "weekly next week",
			payload: aiProactivePayload{Cadence: "weekly", Time: "09:00", Weekday: 5, Timezone: "Asia/Shanghai"},
			after:   at("2026-10-30 10:00"),
			want:    "2026-11-06 09:00",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextAIProactiveRun(test.payload, test.after)
			if err != nil {
				t.Fatal(err)
			}
			if formatted := got.In(loc).Format("2006-01-02 15:04"); formatted != test.want {
				t.Fatalf("got %s, want %s", formatted, test.want)
			}
		})
	}
}

func TestNormalizeAIProactiveOnce(t *testing.T) {
	user := User{Timezone: "Asia/Shanghai"}
	runAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
	payload, got, err := normalizeAIProactive(user, aiProactiveInput{
		Kind: "greeting", Cadence: "once", RunAt: runAt.Format(time.RFC3339), TeamID: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Cadence != "once" || !payload.Enabled {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Time == "" || payload.Weekday < 1 || payload.Weekday > 7 {
		t.Fatalf("once payload missing local time/weekday: %+v", payload)
	}
	if !got.Equal(runAt) {
		t.Fatalf("got runAt %s, want %s", got, runAt)
	}
}
