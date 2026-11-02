package live

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// transcribeWechatVoice bridges the plugin's SILK→WAV output into the
// configured STT provider. WeChat's own `voice_item.text` is used first by
// inboundText and never reaches this path.
func (s *Server) transcribeWechatVoice(ctx context.Context, gateway *openClawWechatGateway, message pluginWechatMessage) (string, error) {
	media := wechatVoiceMedia(message)
	if media == nil {
		return "", errors.New("voice media missing")
	}
	audio, mimeType, err := gateway.downloadVoice(ctx, media)
	if err != nil {
		return "", err
	}
	if mimeType != "audio/wav" && mimeType != "audio/x-wav" && mimeType != "audio/wave" {
		return "", fmt.Errorf("unsupported voice codec %s", mimeType)
	}
	return s.transcribeAudio(ctx, audio, mimeType)
}

func (s *Server) transcribeAudio(ctx context.Context, audio []byte, mimeType string) (string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("STT_BASE_URL")), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	key := strings.TrimSpace(os.Getenv("STT_API_KEY"))
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("STT_PROVIDER")))
	if provider == "" && strings.Contains(base, "xiaomimimo.com") {
		provider = "mimo"
	}
	model := strings.TrimSpace(os.Getenv("STT_MODEL"))
	if model == "" {
		if provider == "mimo" {
			model = "mimo-v2.5-asr"
		} else {
			model = "whisper-1"
		}
	}
	if provider == "mimo" {
		return transcribeMiMo(ctx, base, key, model, audio, mimeType)
	}
	if key == "" {
		parsed, parseErr := url.Parse(base)
		host := ""
		if parseErr == nil {
			host = parsed.Hostname()
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return "", errors.New("stt_not_configured: set STT_API_KEY/STT_BASE_URL/STT_MODEL")
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "voice.wav")
	if err != nil {
		return "", err
	}
	if _, err = part.Write(audio); err != nil {
		return "", err
	}
	if err = writer.WriteField("model", model); err != nil {
		return "", err
	}
	if language := strings.TrimSpace(os.Getenv("STT_LANGUAGE")); language != "" {
		_ = writer.WriteField("language", language)
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 90 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("stt status %d: %s", response.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 160))
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &result) != nil || strings.TrimSpace(result.Text) == "" {
		return "", errors.New("stt returned empty transcript")
	}
	return truncateRunes(strings.TrimSpace(result.Text), 12000), nil
}

// MiMo ASR uses OpenAI-compatible chat completions with an input_audio part,
// not the /audio/transcriptions endpoint. MP3 is substantially smaller than
// the WAV produced by the WeChat plugin, so convert when ffmpeg is available.
func transcribeMiMo(ctx context.Context, base, key, model string, audio []byte, mimeType string) (string, error) {
	if key == "" {
		return "", errors.New("stt_not_configured: set STT_API_KEY for MiMo ASR")
	}
	audio, mimeType = wavToMP3(ctx, audio, mimeType)
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(audio)
	language := strings.TrimSpace(os.Getenv("STT_LANGUAGE"))
	if language == "" {
		language = "zh"
	}
	payload, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type": "input_audio", "input_audio": map[string]string{"data": dataURI},
			}},
		}},
		"asr_options": map[string]string{"language": language},
		"stream":      false,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-key", key)
	request.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 120 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("mimo asr status %d: %s", response.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 200))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("mimo asr returned empty transcript")
	}
	return truncateRunes(strings.TrimSpace(result.Choices[0].Message.Content), 12000), nil
}

func wavToMP3(ctx context.Context, audio []byte, mimeType string) ([]byte, string) {
	if !strings.Contains(strings.ToLower(mimeType), "wav") {
		return audio, mimeType
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return audio, mimeType
	}
	dir, err := os.MkdirTemp("", "teammind-stt-")
	if err != nil {
		return audio, mimeType
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "voice.wav")
	out := filepath.Join(dir, "voice.mp3")
	if os.WriteFile(in, audio, 0600) != nil {
		return audio, mimeType
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", in, "-codec:a", "libmp3lame", "-qscale:a", "4", out)
	if cmd.Run() != nil {
		return audio, mimeType
	}
	converted, err := os.ReadFile(out)
	if err != nil || len(converted) == 0 {
		return audio, mimeType
	}
	return converted, "audio/mpeg"
}

func sttConfigured() bool {
	base := strings.TrimSpace(os.Getenv("STT_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("STT_API_KEY"))
	if base == "" {
		return false
	}
	if key != "" {
		return true
	}
	parsed, err := url.Parse(base)
	return err == nil && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1")
}
