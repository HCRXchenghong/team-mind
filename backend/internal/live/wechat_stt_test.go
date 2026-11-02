package live

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestInboundTextUsesVoiceTranscript(t *testing.T) {
	var message pluginWechatMessage
	if err := json.Unmarshal([]byte(`{"item_list":[{"type":3,"voice_item":{"text":"微信自带转写"}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if got := inboundText(message); got != "微信自带转写" {
		t.Fatalf("voice transcript not used: %q", got)
	}
	var mediaMessage pluginWechatMessage
	if err := json.Unmarshal([]byte(`{"item_list":[{"type":3,"voice_item":{"media":{"encrypt_query_param":"q","aes_key":"a2V5"}}}]}`), &mediaMessage); err != nil {
		t.Fatal(err)
	}
	media := wechatVoiceMedia(mediaMessage)
	if media["encrypt_query_param"] != "q" || media["aes_key"] != "a2V5" {
		t.Fatalf("voice media not parsed: %#v", media)
	}
}

func TestTranscribeAudioOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "multipart", http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "fake-wav" || r.FormValue("model") != "whisper-test" {
			http.Error(w, "payload", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "你好，这是语音转写。"})
	}))
	defer server.Close()
	t.Setenv("STT_BASE_URL", server.URL)
	t.Setenv("STT_API_KEY", "test-key")
	t.Setenv("STT_MODEL", "whisper-test")
	text, err := (&Server{}).transcribeAudio(context.Background(), []byte("fake-wav"), "audio/wav")
	if err != nil || !strings.Contains(text, "语音转写") {
		t.Fatalf("stt failed: text=%q err=%v", text, err)
	}
}

func TestTranscribeWechatVoiceDownloadsAndTranscribes(t *testing.T) {
	voice := []byte("wav-bytes")
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channel/download-voice" || r.Header.Get("Authorization") != "Bearer bridge-token" {
			http.Error(w, "bad bridge request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"audioBase64": base64.StdEncoding.EncodeToString(voice),
			"mime":        "audio/wav",
		})
	}))
	defer bridge.Close()
	stt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "微信语音已转写"})
	}))
	defer stt.Close()
	t.Setenv("STT_BASE_URL", stt.URL)
	t.Setenv("STT_API_KEY", "stt-key")
	t.Setenv("STT_MODEL", "whisper-test")
	var message pluginWechatMessage
	if err := json.Unmarshal([]byte(`{"item_list":[{"type":3,"voice_item":{"media":{"encrypt_query_param":"q","aes_key":"a2V5"}}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	gateway := &openClawWechatGateway{baseURL: bridge.URL, token: "bridge-token", client: bridge.Client()}
	text, err := (&Server{}).transcribeWechatVoice(context.Background(), gateway, message)
	if err != nil || text != "微信语音已转写" {
		t.Fatalf("voice pipeline failed: text=%q err=%v", text, err)
	}
}

func TestMiMoASRLive(t *testing.T) {
	key := os.Getenv("STT_LIVE_KEY")
	audioPath := os.Getenv("STT_LIVE_AUDIO")
	if key == "" || audioPath == "" {
		t.Skip("STT_LIVE_KEY/STT_LIVE_AUDIO not configured")
	}
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STT_PROVIDER", "mimo")
	t.Setenv("STT_BASE_URL", "https://api.xiaomimimo.com/v1")
	t.Setenv("STT_API_KEY", key)
	t.Setenv("STT_MODEL", "mimo-v2.5-asr")
	t.Setenv("STT_LANGUAGE", "zh")
	text, err := (&Server{}).transcribeAudio(context.Background(), audio, "audio/wav")
	if err != nil || !strings.Contains(text, "TeamMind") {
		t.Fatalf("mimo asr failed: text=%q err=%v", text, err)
	}
}
