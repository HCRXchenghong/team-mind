package live

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

func makeTestSubscription(t *testing.T, endpoint string) (browserPushSubscription, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), private.X, private.Y)
	auth := make([]byte, 16)
	if _, err = rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	sub := browserPushSubscription{Endpoint: endpoint}
	sub.Keys.P256DH = base64.RawURLEncoding.EncodeToString(public)
	sub.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return sub, private, auth
}

func TestVAPIDSubjectRejectsPlaceholdersAndAcceptsRealContactForms(t *testing.T) {
	for _, subject := range []string{
		"mailto:admin@example.invalid",
		"mailto:admin@example.com",
		"mailto:admin@example.test",
		"mailto:admin@localhost",
		"mailto:admin@invalid..org",
		"mailto:not-an-address",
		"https://example.org/security",
		"https://ops.example.com/contact",
		"https://service.example.invalid/contact",
		"https://127.0.0.1/contact",
		"https://push.local/contact",
		"https://push_domain.org/contact",
		"http://team-mind.org/security",
		"https://user:password@team-mind.org/contact",
	} {
		if validVAPIDSubject(subject) {
			t.Errorf("placeholder or invalid VAPID subject accepted: %q", subject)
		}
	}
	for _, subject := range []string{
		"mailto:push-ops@team-mind.org",
		"https://team-mind.org/security/contact",
	} {
		if !validVAPIDSubject(subject) {
			t.Errorf("valid VAPID contact rejected: %q", subject)
		}
	}
}

func TestNotificationPushConfiguredRejectsPlaceholderSubject(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAPID_PRIVATE_KEY", base64.RawURLEncoding.EncodeToString(private.D.FillBytes(make([]byte, 32))))
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	for _, subject := range []string{"mailto:admin@example.invalid", "mailto:admin@example.com", "https://example.org/contact"} {
		t.Setenv("VAPID_SUBJECT", subject)
		if notificationPushConfigured() {
			t.Errorf("placeholder VAPID subject advertised configured push: %q", subject)
		}
	}
	t.Setenv("VAPID_SUBJECT", "mailto:push-ops@team-mind.org")
	if !notificationPushConfigured() {
		t.Fatal("valid VAPID subject was not accepted")
	}
}

func TestNotificationChannelStatusDoesNotAdvertisePlaceholderVAPID(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires a migrated MYSQL_DSN: %v", err)
	}
	defer server.DB.Close()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAPID_PRIVATE_KEY", base64.RawURLEncoding.EncodeToString(private.D.FillBytes(make([]byte, 32))))
	t.Setenv("VAPID_SUBJECT", "mailto:admin@example.invalid")
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/notification-channels", nil)
	if err = server.notificationChannels(recorder, request, User{ID: id()}, "me/notification-channels"); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Code string         `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "OK" || response.Data["pushConfigured"] != false || response.Data["pushPublicKey"] != "" {
		t.Fatalf("channel status advertised placeholder VAPID configuration: %s", recorder.Body.String())
	}
}

func TestEncryptedPushSubscriptionBindsOwnerAndEndpoint(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	sub, _, _ := makeTestSubscription(t, "https://fcm.googleapis.com/send/private-endpoint")
	raw, _ := json.Marshal(sub)
	id, digest, encrypted, err := encryptPushSubscription("abcdef0123456789abcdef0123456789", sub.Endpoint, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !validID(id) || strings.Contains(string(encrypted), sub.Endpoint) {
		t.Fatal("subscription was not encrypted")
	}
	decoded, err := decryptPushSubscription("abcdef0123456789abcdef0123456789", digest, encrypted)
	if err != nil || decoded.Endpoint != sub.Endpoint || decoded.Keys.P256DH != sub.Keys.P256DH {
		t.Fatalf("decrypted subscription=%+v err=%v", decoded, err)
	}
	if _, err = decryptPushSubscription("00000000000000000000000000000000", digest, encrypted); err == nil {
		t.Fatal("ciphertext decrypted for another owner")
	}
}

func TestWebPushEncryptionCanBeDecryptedByBrowserKey(t *testing.T) {
	sub, recipient, auth := makeTestSubscription(t, "https://fcm.googleapis.com/send/test")
	payload := []byte(`{"title":"TeamMind","body":"New notification"}`)
	encoded, err := encryptWebPush(sub, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 21 || encoded[20] != 65 {
		t.Fatalf("invalid RFC 8188 header length: %d", len(encoded))
	}
	keyLength := int(encoded[20])
	ephemeralBytes := encoded[21 : 21+keyLength]
	ephemeralX, ephemeralY := elliptic.Unmarshal(elliptic.P256(), ephemeralBytes)
	if ephemeralX == nil {
		t.Fatal("invalid ephemeral public key")
	}
	sharedX, _ := elliptic.P256().ScalarMult(ephemeralX, ephemeralY, recipient.D.Bytes())
	shared := sharedX.FillBytes(make([]byte, 32))
	uaPublic, _ := base64.RawURLEncoding.DecodeString(sub.Keys.P256DH)
	keyInfo := append([]byte("WebPush: info\x00"), uaPublic...)
	keyInfo = append(keyInfo, ephemeralBytes...)
	ikm := hkdfExpand(hkdfExtract(auth, shared), keyInfo, 32)
	prk := hkdfExtract(encoded[:16], ikm)
	contentKey := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	block, _ := aes.NewCipher(contentKey)
	gcm, _ := cipher.NewGCM(block)
	ciphertext := encoded[21+keyLength:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil || len(plain) == 0 || plain[len(plain)-1] != 2 || string(plain[:len(plain)-1]) != string(payload) {
		t.Fatalf("web push decrypt failed: plaintext=%q err=%v", plain, err)
	}
}

func TestSendWebPushUsesVAPIDAndEncryptedPayload(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAPID_PRIVATE_KEY", base64.RawURLEncoding.EncodeToString(private.D.FillBytes(make([]byte, 32))))
	t.Setenv("VAPID_SUBJECT", "mailto:push-ops@team-mind.org")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("APP_ENV", "development")
	var received bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodPost || r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("TTL") != "86400" {
			t.Errorf("incorrect web push request headers: %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) < 100 {
			t.Errorf("encrypted push body is too short: %d", len(body))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	sub, _, _ := makeTestSubscription(t, server.URL+"/push/test")
	status, err := sendWebPush(context.Background(), sub, []byte(`{"body":"generic"}`))
	if err != nil || status != http.StatusCreated || !received {
		t.Fatalf("status=%d received=%v err=%v", status, received, err)
	}
}

func TestNotificationEmailRequiresSTARTTLSAndSendsGenericLink(t *testing.T) {
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(17), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &serverKey.PublicKey, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}))
	if err != nil {
		t.Fatal(err)
	}
	parsedCertificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsedCertificate)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	message := make(chan string, 1)
	serverErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer conn.Close()
		proto := textproto.NewConn(conn)
		if acceptErr = proto.PrintfLine("220 localhost ready"); acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		var body strings.Builder
		for {
			line, readErr := proto.ReadLine()
			if readErr != nil {
				serverErr <- readErr
				return
			}
			command := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(command, "EHLO ") || strings.HasPrefix(command, "HELO "):
				_ = proto.PrintfLine("250-localhost")
				_ = proto.PrintfLine("250-STARTTLS")
				_ = proto.PrintfLine("250 AUTH PLAIN")
			case command == "STARTTLS":
				_ = proto.PrintfLine("220 begin TLS")
				tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				if err := tlsConn.Handshake(); err != nil {
					serverErr <- err
					return
				}
				conn = tlsConn
				proto = textproto.NewConn(conn)
			case strings.HasPrefix(command, "AUTH PLAIN"):
				_ = proto.PrintfLine("235 authenticated")
			case strings.HasPrefix(command, "MAIL FROM:") || strings.HasPrefix(command, "RCPT TO:"):
				_ = proto.PrintfLine("250 accepted")
			case command == "DATA":
				_ = proto.PrintfLine("354 end with dot")
				for {
					dataLine, dataErr := proto.ReadLine()
					if dataErr != nil {
						serverErr <- dataErr
						return
					}
					if dataLine == "." {
						break
					}
					body.WriteString(dataLine)
					body.WriteByte('\n')
				}
				message <- body.String()
				_ = proto.PrintfLine("250 queued")
			case command == "QUIT":
				_ = proto.PrintfLine("221 bye")
				serverErr <- nil
				return
			default:
				_ = proto.PrintfLine("250 accepted")
			}
		}
	}()
	t.Setenv("SMTP_HOST", "localhost")
	t.Setenv("SMTP_PORT", port)
	t.Setenv("SMTP_USER", "test-user")
	t.Setenv("SMTP_PASSWORD", "test-password")
	t.Setenv("SMTP_FROM", "TeamMind <notify@example.test>")
	t.Setenv("PUBLIC_URL", "https://team.example.test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sendNotificationEmailWithTLS(ctx, "person@example.test", "https://team.example.test", &tls.Config{ServerName: "localhost", RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("SMTP STARTTLS delivery failed: %v", err)
	}
	select {
	case got := <-message:
		if !strings.Contains(got, "https://team.example.test/teammind/notifications") || strings.Contains(got, "task secret") || strings.Contains(got, "chat secret") {
			t.Fatalf("notification email exposed content or missed the inbox link: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP peer did not receive the notification")
	}
	if serverFailure := <-serverErr; serverFailure != nil {
		t.Fatalf("SMTP peer failed: %v", serverFailure)
	}
}

func TestPushSubscriptionRejectsMalformedKeysAndNonHTTPSEndpoints(t *testing.T) {
	sub, _, _ := makeTestSubscription(t, "https://fcm.googleapis.com/send/test")
	if err := validatePushKeys(sub); err != nil {
		t.Fatal(err)
	}
	bad := sub
	bad.Keys.Auth = "not-a-secret"
	if err := validatePushKeys(bad); err == nil {
		t.Fatal("invalid auth key accepted")
	}
	bad = sub
	bad.Endpoint = "https://127.0.0.1/private"
	if err := validatePushSubscription(context.Background(), bad, false); err == nil {
		t.Fatal("private push endpoint accepted")
	}
	bad = sub
	bad.Endpoint = "http://push.example.invalid/endpoint"
	if err := validatePushSubscription(context.Background(), bad, false); err == nil {
		t.Fatal("insecure push endpoint accepted")
	}
}

func TestPushEndpointAddressPolicyBlocksSpecialRanges(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1", "10.1.2.3", "100.64.1.2", "169.254.169.254", "192.0.2.5",
		"198.18.0.1", "203.0.113.4", "::1", "fe80::1", "2001:db8::1", "64:ff9b::a00:1",
	} {
		if isPublicPushIP(net.ParseIP(raw)) {
			t.Errorf("non-public address %s accepted", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		if !isPublicPushIP(net.ParseIP(raw)) {
			t.Errorf("public address %s rejected", raw)
		}
	}
}

func TestSandboxRunsOfflineWithTemporaryFiles(t *testing.T) {
	if !agentSandboxAvailable() {
		t.Skip("bubblewrap sandbox needs a non-root account, prlimit and Python 3")
	}
	result, err := runPythonSandbox(context.Background(), "import os, socket\ntry:\n socket.create_connection(('1.1.1.1',443),0.2)\n print('network-open')\nexcept OSError:\n print('network-blocked')\ntry:\n open('/escape-test.txt','w').close()\n print('rootfs-writable')\nexcept OSError:\n print('rootfs-readonly')\nwith open('result.txt','w') as f: f.write(open('input.txt').read().upper())\n", []sandboxInputFile{{Name: "input.txt", Content: "safe input"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "network-blocked") || strings.Contains(result.Stdout, "network-open") {
		t.Fatalf("network namespace did not block outbound access: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "rootfs-readonly") || strings.Contains(result.Stdout, "rootfs-writable") {
		t.Fatalf("sandbox root filesystem was writable: %q", result.Stdout)
	}
	if result.Files["result.txt"] != "SAFE INPUT" || result.CodeHash == "" {
		t.Fatalf("sandbox output=%+v", result)
	}
	if _, err = runPythonSandbox(context.Background(), "print('bad')", []sandboxInputFile{{Name: "../escape", Content: "x"}}); err == nil {
		t.Fatal("sandbox accepted a path escape")
	}
}

func TestSandboxBoundsAggregateTemporaryWorkspace(t *testing.T) {
	if !agentSandboxAvailable() {
		t.Skip("bubblewrap sandbox needs a non-root account, prlimit and Python 3")
	}
	if err := checkSandboxWorkspace(t.TempDir()); err != nil {
		t.Fatalf("empty workspace should be within limits: %v", err)
	}
	_, err := runPythonSandbox(context.Background(), "for i in range(256):\n open(f'file-{i}', 'w').close()\n", nil)
	if err == nil || !strings.Contains(err.Error(), "工作区") {
		t.Fatalf("sandbox did not stop excessive temporary files: %v", err)
	}
}

func TestSandboxInputCannotReplaceEntryPointOrUseDuplicatePaths(t *testing.T) {
	if err := validateSandboxInput("print('safe')", []sandboxInputFile{{Name: "main.py", Content: "print('replace')"}}); err == nil {
		t.Fatal("sandbox accepted a file that overwrites its program")
	}
	if err := validateSandboxInput("print('safe')", []sandboxInputFile{{Name: "input.txt", Content: "one"}, {Name: "input.txt", Content: "two"}}); err == nil {
		t.Fatal("sandbox accepted duplicate input paths")
	}
	if err := validateSandboxInput("print('safe')", []sandboxInputFile{{Name: "data/input.txt", Content: "safe"}}); err != nil {
		t.Fatalf("valid workspace-relative input was rejected: %v", err)
	}
}

func TestAgentToolSelectionAdvertisesOnlyEnabledCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Parallel bool `json:"parallel_tool_calls"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Parallel || len(request.Tools) != 6 || request.Tools[0].Function.Name != "list_team_members" || request.Tools[1].Function.Name != "list_team_tasks" || request.Tools[2].Function.Name != "list_team_projects" || request.Tools[3].Function.Name != "propose_task_change" || request.Tools[4].Function.Name != "propose_project_change" || request.Tools[5].Function.Name != "propose_member_change" {
			t.Errorf("unsafe tool configuration: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"checking tasks","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_team_tasks","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()
	t.Setenv("AI_BASE_URL", server.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	model, content, calls, err := requestAgentToolChoice(context.Background(), []aiMessage{{Role: "user", Content: "show tasks"}}, true, false)
	if err != nil || model != "test-model" || content != "checking tasks" || len(calls) != 1 || calls[0].Function.Name != "list_team_tasks" {
		t.Fatalf("model=%s content=%s calls=%+v err=%v", model, content, calls, err)
	}
}

func TestVAPIDTokenHasAudienceAndShortExpiry(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := &vapidKeys{Private: private, Public: elliptic.Marshal(elliptic.P256(), private.X, private.Y)}
	now := time.Unix(1_800_000_000, 0)
	jwt, err := vapidToken(keys, "https://push.example.test", " mailto:push-ops@team-mind.org ", now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT: %s", jwt)
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var value struct {
		Audience string `json:"aud"`
		Exp      int64  `json:"exp"`
		Subject  string `json:"sub"`
	}
	if err = json.Unmarshal(claims, &value); err != nil || value.Audience != "https://push.example.test" || value.Exp != now.Add(12*time.Hour).Unix() || value.Subject != "mailto:push-ops@team-mind.org" {
		t.Fatalf("claims=%+v err=%v", value, err)
	}
	if len(parts[2]) == 0 || new(big.Int).SetBytes(keys.Public[:32]).Sign() == 0 {
		t.Fatal("JWT or public key empty")
	}
}
