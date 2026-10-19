package live

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type browserPushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type vapidKeys struct {
	Private *ecdsa.PrivateKey
	Public  []byte
}

func decodeBase64Key(value string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := enc.DecodeString(strings.TrimSpace(value)); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64 key")
}

func notificationEncryptionKey() ([]byte, error) {
	key, err := decodeBase64Key(os.Getenv("NOTIFICATION_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("NOTIFICATION_ENCRYPTION_KEY must be a base64 encoded 32 byte key")
	}
	return key, nil
}

func notificationPushKeys() (*vapidKeys, error) {
	d, err := decodeBase64Key(os.Getenv("VAPID_PRIVATE_KEY"))
	if err != nil || len(d) != 32 {
		return nil, errors.New("VAPID_PRIVATE_KEY must be a base64 encoded 32 byte P-256 private key")
	}
	curve := elliptic.P256()
	x, y := curve.ScalarBaseMult(d)
	if x == nil || y == nil {
		return nil, errors.New("invalid VAPID private key")
	}
	private := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: new(big.Int).SetBytes(d)}
	return &vapidKeys{Private: private, Public: elliptic.Marshal(curve, x, y)}, nil
}

func notificationEmailConfigured() bool {
	return os.Getenv("SMTP_HOST") != "" && os.Getenv("SMTP_USER") != "" && os.Getenv("SMTP_PASSWORD") != "" && os.Getenv("SMTP_FROM") != "" && os.Getenv("PUBLIC_URL") != ""
}

// validVAPIDSubject rejects syntactically invalid contacts and the example or
// reserved domains commonly left in deployment templates. A placeholder must
// never make the UI advertise push as configured: browsers may reject its
// VAPID authorization even though the key material itself is valid.
func validVAPIDSubject(subject string) bool {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(strings.ToLower(subject), "mailto:") {
		address := strings.TrimSpace(subject[len("mailto:"):])
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "?/#") {
			return false
		}
		at := strings.LastIndexByte(address, '@')
		return at > 0 && !placeholderContactDomain(address[at+1:])
	}
	parsed, err := url.Parse(subject)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	return !placeholderContactDomain(parsed.Hostname())
}

func placeholderContactDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || len(domain) > 253 || net.ParseIP(domain) != nil {
		return true
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return true
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return true
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return true
			}
		}
	}
	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") || strings.HasSuffix(domain, ".local") {
		return true
	}
	for _, suffix := range []string{".invalid", ".example", ".test", ".example.com", ".example.net", ".example.org", ".arpa"} {
		if domain == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}

func notificationPushConfigured() bool {
	_, keyErr := notificationPushKeys()
	_, encErr := notificationEncryptionKey()
	subject := strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	return keyErr == nil && encErr == nil && validVAPIDSubject(subject)
}

func (s *Server) notificationChannels(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(path, "/")
	if len(parts) == 2 && r.Method == http.MethodGet {
		var emailEnabled, pushEnabled, nativePushEnabled bool
		var subscriptions, nativeDevices int
		err := s.DB.QueryRowContext(r.Context(), `SELECT email_enabled,push_enabled,native_push_enabled FROM notification_preferences WHERE user_id=UNHEX(?)`, u.ID).Scan(&emailEnabled, &pushEnabled, &nativePushEnabled)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active'`, u.ID).Scan(&subscriptions); err != nil {
			return err
		}
		if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active'`, u.ID).Scan(&nativeDevices); err != nil {
			return err
		}
		var publicKey string
		if keys, err := notificationPushKeys(); err == nil && notificationPushConfigured() {
			publicKey = base64.RawURLEncoding.EncodeToString(keys.Public)
		}
		write(w, http.StatusOK, map[string]any{
			"emailConfigured": notificationEmailConfigured(), "emailEnabled": emailEnabled,
			"pushConfigured": notificationPushConfigured(), "pushEnabled": pushEnabled,
			"pushPublicKey": publicKey, "pushSubscriptions": subscriptions,
			"nativePushConfigured": nativePushConfigured(), "nativePushEnabled": nativePushEnabled, "nativeDevices": nativeDevices,
		})
		return nil
	}
	if len(parts) == 3 && parts[2] == "push-subscriptions" && r.Method == http.MethodPost {
		if !notificationPushConfigured() {
			return bad(http.StatusServiceUnavailable, "PUSH_CHANNEL_NOT_CONFIGURED", "浏览器推送服务尚未配置")
		}
		var sub browserPushSubscription
		if err := body(w, r, &sub); err != nil {
			return err
		}
		if err := validatePushSubscription(r.Context(), sub, s.development); err != nil {
			return bad(http.StatusBadRequest, "INVALID_PUSH_SUBSCRIPTION", "浏览器推送订阅无效")
		}
		raw, err := json.Marshal(sub)
		if err != nil {
			return err
		}
		subID, endpointHash, encrypted, err := encryptPushSubscription(u.ID, sub.Endpoint, raw)
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var existingID, existingOwner, existingStatus string
		err = tx.QueryRowContext(r.Context(), `SELECT HEX(id),HEX(user_id),status FROM notification_push_subscriptions WHERE endpoint_hash=? FOR UPDATE`, endpointHash).Scan(&existingID, &existingOwner, &existingStatus)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && !strings.EqualFold(existingOwner, u.ID) && existingStatus == "active" {
			return bad(http.StatusConflict, "PUSH_SUBSCRIPTION_IN_USE", "此浏览器推送订阅已属于其他账号")
		}
		if err == sql.ErrNoRows {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO notification_push_subscriptions(id,user_id,endpoint_hash,encrypted_subscription,status) VALUES(UNHEX(?),UNHEX(?),?,?, 'active')`, subID, u.ID, endpointHash, encrypted)
		} else {
			if !strings.EqualFold(existingOwner, u.ID) {
				if _, err = tx.ExecContext(r.Context(), `UPDATE notification_deliveries SET status='suppressed',last_error='subscription_reassigned',lease_until=NULL WHERE subscription_id=UNHEX(?) AND status IN ('queued','retry','sending')`, existingID); err != nil {
					return err
				}
				if _, err = tx.ExecContext(r.Context(), `UPDATE notification_preferences SET push_enabled=0 WHERE user_id=UNHEX(?)`, existingOwner); err != nil {
					return err
				}
			}
			subID = existingID
			_, err = tx.ExecContext(r.Context(), `UPDATE notification_push_subscriptions SET user_id=UNHEX(?),encrypted_subscription=?,status='active' WHERE id=UNHEX(?)`, u.ID, encrypted, existingID)
		}
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		write(w, http.StatusCreated, map[string]string{"id": subID})
		return nil
	}
	if len(parts) == 3 && parts[2] == "push-subscriptions" && r.Method == http.MethodDelete {
		var input struct {
			Endpoint string `json:"endpoint"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		if input.Endpoint == "" || len(input.Endpoint) > 2048 {
			return bad(http.StatusBadRequest, "INVALID_PUSH_SUBSCRIPTION", "推送订阅无效")
		}
		digest := sha256.Sum256([]byte(input.Endpoint))
		res, err := s.DB.ExecContext(r.Context(), `UPDATE notification_push_subscriptions SET status='revoked' WHERE endpoint_hash=? AND user_id=UNHEX(?) AND status='active'`, digest[:], u.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "当前设备没有有效推送订阅")
		}
		_, err = s.DB.ExecContext(r.Context(), `UPDATE notification_preferences SET push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active')`, u.ID, u.ID)
		if err != nil {
			return err
		}
		write(w, http.StatusOK, map[string]bool{"revoked": true})
		return nil
	}
	if len(parts) == 4 && parts[2] == "push-subscriptions" && validID(parts[3]) && r.Method == http.MethodDelete {
		res, err := s.DB.ExecContext(r.Context(), `UPDATE notification_push_subscriptions SET status='revoked' WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, parts[3], u.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "推送订阅不存在")
		}
		_, err = s.DB.ExecContext(r.Context(), `UPDATE notification_preferences SET push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active')`, u.ID, u.ID)
		if err != nil {
			return err
		}
		write(w, http.StatusOK, map[string]bool{"revoked": true})
		return nil
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func validatePushSubscription(ctx context.Context, sub browserPushSubscription, development bool) error {
	if len(sub.Endpoint) > 2048 || sub.Keys.P256DH == "" || sub.Keys.Auth == "" {
		return errors.New("missing subscription fields")
	}
	endpoint, err := url.ParseRequestURI(sub.Endpoint)
	if err != nil || endpoint.User != nil || endpoint.Hostname() == "" || endpoint.Opaque != "" || endpoint.Fragment != "" || strings.Contains(endpoint.Hostname(), "%") {
		return errors.New("invalid endpoint")
	}
	if development && endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost" || endpoint.Hostname() == "::1") {
		return validatePushKeys(sub)
	}
	if endpoint.Scheme != "https" || endpoint.Port() != "" && endpoint.Port() != "443" {
		return errors.New("push endpoint must use HTTPS on port 443")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, endpoint.Hostname())
	if err != nil || len(ips) == 0 {
		return errors.New("push endpoint host cannot be resolved")
	}
	for _, item := range ips {
		if !isPublicPushIP(item.IP) {
			return errors.New("push endpoint resolved to a non-public address")
		}
	}
	return validatePushKeys(sub)
}

// Push endpoints come from the browser and are attacker-controlled. Apply the
// same public-only policy at registration and immediately before dialing, so
// DNS rebinding cannot turn the notification worker into an SSRF proxy.
func isPublicPushIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	blocked := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
		"2002::/16", "3fff::/20",
	}
	for _, raw := range blocked {
		prefix, _ := netip.ParsePrefix(raw)
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validatePushKeys(sub browserPushSubscription) error {
	publicKey, err := base64.RawURLEncoding.DecodeString(sub.Keys.P256DH)
	if err != nil || len(publicKey) != 65 || publicKey[0] != 4 {
		return errors.New("invalid P-256 subscription key")
	}
	if x, y := elliptic.Unmarshal(elliptic.P256(), publicKey); x == nil || y == nil {
		return errors.New("invalid P-256 subscription key")
	}
	auth, err := base64.RawURLEncoding.DecodeString(sub.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("invalid subscription auth secret")
	}
	return nil
}

func encryptPushSubscription(userID, endpoint string, raw []byte) (string, []byte, []byte, error) {
	key, err := notificationEncryptionKey()
	if err != nil {
		return "", nil, nil, err
	}
	endpointHash := sha256.Sum256([]byte(endpoint))
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", nil, nil, err
	}
	aad := []byte(strings.ToLower(userID) + ":" + hex.EncodeToString(endpointHash[:]))
	ciphertext := gcm.Seal(nonce, nonce, raw, aad)
	return id(), endpointHash[:], ciphertext, nil
}

func decryptPushSubscription(userID string, endpointHash, ciphertext []byte) (browserPushSubscription, error) {
	var sub browserPushSubscription
	key, err := notificationEncryptionKey()
	if err != nil {
		return sub, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return sub, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return sub, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return sub, errors.New("invalid encrypted subscription")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], []byte(strings.ToLower(userID)+":"+hex.EncodeToString(endpointHash)))
	if err != nil {
		return sub, err
	}
	if err = json.Unmarshal(plain, &sub); err != nil {
		return sub, err
	}
	return sub, nil
}

func vapidToken(keys *vapidKeys, audience, subject string, now time.Time) (string, error) {
	subject = strings.TrimSpace(subject)
	if !validVAPIDSubject(subject) {
		return "", errors.New("VAPID_SUBJECT must be a valid mailto contact or HTTPS contact URL, not a placeholder domain")
	}
	header, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims, _ := json.Marshal(map[string]any{"aud": audience, "exp": now.Add(12 * time.Hour).Unix(), "sub": subject})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, keys.Private, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func hkdfExtract(salt, input []byte) []byte {
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write(input)
	return mac.Sum(nil)
}

func hkdfExpand(prk, info []byte, size int) []byte {
	var output, previous []byte
	for counter := byte(1); len(output) < size; counter++ {
		mac := hmac.New(sha256.New, prk)
		_, _ = mac.Write(previous)
		_, _ = mac.Write(info)
		_, _ = mac.Write([]byte{counter})
		previous = mac.Sum(nil)
		output = append(output, previous...)
	}
	return output[:size]
}

func encryptWebPush(sub browserPushSubscription, payload []byte) ([]byte, error) {
	uaPublic, err := base64.RawURLEncoding.DecodeString(sub.Keys.P256DH)
	if err != nil {
		return nil, err
	}
	curve := elliptic.P256()
	uaX, uaY := elliptic.Unmarshal(curve, uaPublic)
	if uaX == nil || uaY == nil {
		return nil, errors.New("invalid subscription public key")
	}
	auth, err := base64.RawURLEncoding.DecodeString(sub.Keys.Auth)
	if err != nil {
		return nil, err
	}
	ephemeral, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, err
	}
	ephemeralPublic := elliptic.Marshal(curve, ephemeral.X, ephemeral.Y)
	sharedX, _ := curve.ScalarMult(uaX, uaY, ephemeral.D.Bytes())
	if sharedX == nil {
		return nil, errors.New("invalid ECDH shared secret")
	}
	shared := sharedX.FillBytes(make([]byte, 32))
	keyInfo := append([]byte("WebPush: info\x00"), uaPublic...)
	keyInfo = append(keyInfo, ephemeralPublic...)
	ikm := hkdfExpand(hkdfExtract(auth, shared), keyInfo, 32)
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, err
	}
	prk := hkdfExtract(salt, ikm)
	contentKey := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain := append(append([]byte(nil), payload...), 2)
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	result := make([]byte, 0, 16+4+1+len(ephemeralPublic)+len(ciphertext))
	result = append(result, salt...)
	result = append(result, 0, 0, 16, 0) // RFC 8188 record size: 4096 bytes.
	result = append(result, byte(len(ephemeralPublic)))
	result = append(result, ephemeralPublic...)
	result = append(result, ciphertext...)
	return result, nil
}

func sendWebPush(ctx context.Context, sub browserPushSubscription, payload []byte) (int, error) {
	endpoint, err := url.Parse(sub.Endpoint)
	if err != nil {
		return 0, err
	}
	keys, err := notificationPushKeys()
	if err != nil {
		return 0, err
	}
	if err = validatePushSubscription(ctx, sub, os.Getenv("APP_ENV") != "production"); err != nil {
		return 0, err
	}
	encrypted, err := encryptWebPush(sub, payload)
	if err != nil {
		return 0, err
	}
	audience := endpoint.Scheme + "://" + endpoint.Host
	jwt, err := vapidToken(keys, audience, os.Getenv("VAPID_SUBJECT"), time.Now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(encrypted))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+base64.RawURLEncoding.EncodeToString(keys.Public))
	req.Header.Set("TTL", "86400")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	if endpoint.Scheme == "https" {
		client.Transport = publicHTTPSRoundTripper()
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	return response.StatusCode, nil
}

func publicHTTPSRoundTripper() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("push connection must use public HTTPS")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("push host cannot be resolved")
		}
		var lastErr error
		for _, item := range ips {
			if !isPublicPushIP(item.IP) {
				continue
			}
			conn, dialErr := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("push host resolved to no public addresses")
	}
	return transport
}

func sendNotificationEmail(ctx context.Context, to, publicURL string) error {
	return sendNotificationEmailWithTLS(ctx, to, publicURL, &tls.Config{ServerName: os.Getenv("SMTP_HOST"), MinVersion: tls.VersionTLS12})
}

func sendNotificationEmailWithTLS(ctx context.Context, to, publicURL string, tlsConfig *tls.Config) error {
	if !notificationEmailConfigured() {
		return errors.New("email_not_configured")
	}
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "465"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("invalid_smtp_port")
	}
	from, err := mail.ParseAddress(os.Getenv("SMTP_FROM"))
	if err != nil || strings.ContainsAny(from.Address, "\r\n") {
		return errors.New("invalid_smtp_from")
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil || strings.ContainsAny(recipient.Address, "\r\n") {
		return errors.New("invalid_recipient")
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	var conn net.Conn
	if port == "465" {
		conn, err = (&tlsDialer{host: host, dialer: dialer, config: tlsConfig}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	client, err := smtpNewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp_starttls_required")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if err = client.Auth(smtpPlainAuth("", os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASSWORD"), host)); err != nil {
		return err
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	data, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(data, "From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n你有一条新的 TeamMind 通知，请登录查看：%s/teammind/notifications\r\n", from.String(), recipient.String(), mime.QEncoding.Encode("UTF-8", "TeamMind 新通知"), strings.TrimSuffix(publicURL, "/"))
	if err != nil {
		_ = data.Close()
		return err
	}
	if err = data.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// The SMTP helpers are variables so tests can use a local protocol peer without
// replacing the secure production transport.
var smtpNewClient = smtp.NewClient
var smtpPlainAuth = smtp.PlainAuth

type tlsDialer struct {
	host   string
	dialer *net.Dialer
	config *tls.Config
}

func (d *tlsDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	config := d.config.Clone()
	config.ServerName = d.host
	tlsConn := tls.Client(conn, config)
	if err = tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func classifyPushError(status int, err error) string {
	if status == http.StatusGone || status == http.StatusNotFound {
		return "subscription_expired"
	}
	if err != nil {
		return "push_unavailable"
	}
	return fmt.Sprintf("push_http_%d", status)
}

func pushAudience(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func decryptEncryptedPushSubscription(userID string, endpointHash, ciphertext []byte) (browserPushSubscription, error) {
	return decryptPushSubscription(userID, endpointHash, ciphertext)
}

type notificationDeliveryJob struct {
	ID, UserID, SubscriptionID, NativeDeviceID, Channel string
	Attempts                                            int
	Email                                               string
	EndpointHash, EncryptedSubscription                 []byte
	NativeTokenHash, EncryptedNativeToken               []byte
}

func (s *Server) RunNotificationWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastErrorLog := time.Time{}
	for {
		if err := s.processNotificationDeliveries(ctx); err != nil && ctx.Err() == nil && time.Since(lastErrorLog) >= 30*time.Second {
			log.Printf("notification delivery worker failed; queued messages remain retryable")
			lastErrorLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) processNotificationDeliveries(ctx context.Context) error {
	for i := 0; i < 25; i++ {
		job, err := s.claimNotificationDelivery(ctx)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		status, reason, sendErr, expired := s.deliverNotification(ctx, job)
		if expired && job.SubscriptionID != "" {
			if _, err = s.DB.ExecContext(ctx, `UPDATE notification_push_subscriptions SET status='revoked' WHERE id=UNHEX(?)`, job.SubscriptionID); err != nil {
				return err
			}
			if _, err = s.DB.ExecContext(ctx, `UPDATE notification_preferences SET push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active')`, job.UserID, job.UserID); err != nil {
				return err
			}
		}
		if expired && job.NativeDeviceID != "" {
			if err = s.expireNativePushDevice(ctx, job); err != nil {
				return err
			}
			status, reason, sendErr = "suppressed", "device_token_expired", nil
		}
		if sendErr != nil && job.Attempts < 5 {
			status = "retry"
			reason = classifyDeliveryError(job.Channel, sendErr)
		}
		if sendErr != nil && job.Attempts >= 5 {
			status = "failed"
			reason = classifyDeliveryError(job.Channel, sendErr)
		}
		var retryAt any
		if status == "retry" {
			backoff := time.Duration(30*(1<<(job.Attempts-1))) * time.Second
			if backoff > time.Hour {
				backoff = time.Hour
			}
			retryAt = time.Now().UTC().Add(backoff)
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE notification_deliveries SET status=?,last_error=?,next_retry_at=?,lease_until=NULL,delivered_at=IF(?='delivered',UTC_TIMESTAMP(3),NULL) WHERE id=UNHEX(?) AND status='sending'`, status, reason, retryAt, status, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) expireNativePushDevice(ctx context.Context, job notificationDeliveryJob) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE notification_native_devices SET status='revoked' WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, job.NativeDeviceID, job.UserID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		if _, err = tx.ExecContext(ctx, `UPDATE notification_preferences SET native_push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active')`, job.UserID, job.UserID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.native_push_device_expired','native_push_device',UNHEX(?),JSON_OBJECT('status','revoked'))`, job.UserID, job.NativeDeviceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) claimNotificationDelivery(ctx context.Context) (notificationDeliveryJob, error) {
	var job notificationDeliveryJob
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	var subscriptionID, nativeDeviceID, endpointHash, email sql.NullString
	var nativeTokenHash sql.NullString
	var encrypted, encryptedNative []byte
	err = tx.QueryRowContext(ctx, `SELECT HEX(d.id),HEX(d.recipient_user_id),HEX(d.subscription_id),HEX(d.native_device_id),d.channel,d.attempts,u.email,HEX(s.endpoint_hash),s.encrypted_subscription,HEX(n.token_hash),n.encrypted_token FROM notification_deliveries d JOIN users u ON u.id=d.recipient_user_id LEFT JOIN notification_push_subscriptions s ON s.id=d.subscription_id AND s.user_id=d.recipient_user_id AND s.status='active' LEFT JOIN notification_native_devices n ON n.id=d.native_device_id AND n.user_id=d.recipient_user_id AND n.status='active' WHERE d.channel IN ('email','web_push','native_push') AND ((d.status IN ('queued','retry') AND (d.next_retry_at IS NULL OR d.next_retry_at<=UTC_TIMESTAMP(3))) OR (d.status='sending' AND d.lease_until<UTC_TIMESTAMP(3))) ORDER BY d.created_at,d.id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job.ID, &job.UserID, &subscriptionID, &nativeDeviceID, &job.Channel, &job.Attempts, &email, &endpointHash, &encrypted, &nativeTokenHash, &encryptedNative)
	if err != nil {
		return job, err
	}
	job.SubscriptionID = subscriptionID.String
	job.NativeDeviceID = nativeDeviceID.String
	job.Email = email.String
	if endpointHash.Valid {
		job.EndpointHash, err = hex.DecodeString(endpointHash.String)
		if err != nil {
			return job, err
		}
	}
	job.EncryptedSubscription = encrypted
	if nativeTokenHash.Valid {
		job.NativeTokenHash, err = hex.DecodeString(nativeTokenHash.String)
		if err != nil {
			return job, err
		}
	}
	job.EncryptedNativeToken = encryptedNative
	if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET status='sending',attempts=attempts+1,lease_until=UTC_TIMESTAMP(3)+INTERVAL 2 MINUTE,last_error=NULL WHERE id=UNHEX(?)`, job.ID); err != nil {
		return job, err
	}
	if err = tx.Commit(); err != nil {
		return job, err
	}
	job.Attempts++
	return job, nil
}

func (s *Server) deliverNotification(ctx context.Context, job notificationDeliveryJob) (string, string, error, bool) {
	var userStatus string
	var globallyEnabled, channelEnabled bool
	preferenceColumn := "email_enabled"
	if job.Channel == "web_push" {
		preferenceColumn = "push_enabled"
	} else if job.Channel == "native_push" {
		preferenceColumn = "native_push_enabled"
	}
	query := `SELECT u.status,COALESCE(p.enabled,1),COALESCE(p.` + preferenceColumn + `,0) FROM users u LEFT JOIN notification_preferences p ON p.user_id=u.id WHERE u.id=UNHEX(?)`
	if err := s.DB.QueryRowContext(ctx, query, job.UserID).Scan(&userStatus, &globallyEnabled, &channelEnabled); err != nil {
		return "failed", "recipient_unavailable", err, false
	}
	if userStatus != "active" {
		return "suppressed", "recipient_inactive", nil, false
	}
	if !globallyEnabled || !channelEnabled {
		return "suppressed", "user_disabled", nil, false
	}
	if job.Channel == "email" {
		if !notificationEmailConfigured() {
			return "suppressed", "channel_not_configured", nil, false
		}
		err := sendNotificationEmail(ctx, job.Email, os.Getenv("PUBLIC_URL"))
		if err != nil {
			return "failed", "email_unavailable", err, false
		}
		return "delivered", "", nil, false
	}
	if job.Channel == "native_push" {
		if job.NativeDeviceID == "" || len(job.NativeTokenHash) != 32 || len(job.EncryptedNativeToken) == 0 {
			return "suppressed", "device_unavailable", nil, false
		}
		var deviceActive int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_native_devices WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, job.NativeDeviceID, job.UserID).Scan(&deviceActive); err != nil {
			return "failed", "device_unavailable", err, false
		}
		if deviceActive != 1 {
			return "suppressed", "device_unavailable", nil, false
		}
		if !nativePushConfigured() {
			return "suppressed", "channel_not_configured", nil, false
		}
		deviceToken, err := decryptNativeDeviceToken(job.UserID, job.NativeTokenHash, job.EncryptedNativeToken)
		if err != nil {
			return "failed", "device_decryption_failed", err, false
		}
		account, privateKey, err := loadFirebaseServiceAccount()
		if err != nil {
			return "suppressed", "channel_not_configured", nil, false
		}
		if s.firebase == nil {
			s.firebase = newFirebaseMessagingSender()
		}
		expired, err := s.firebase.send(ctx, account, privateKey, deviceToken, map[string]string{"route": "/teammind/notifications", "event": "notification"})
		if err != nil {
			return "failed", "native_push_unavailable", err, expired
		}
		return "delivered", "", nil, false
	}
	if job.Channel != "web_push" || job.SubscriptionID == "" || len(job.EndpointHash) != 32 || len(job.EncryptedSubscription) == 0 {
		return "suppressed", "subscription_unavailable", nil, false
	}
	if !notificationPushConfigured() {
		return "suppressed", "channel_not_configured", nil, false
	}
	sub, err := decryptPushSubscription(job.UserID, job.EndpointHash, job.EncryptedSubscription)
	if err != nil {
		return "failed", "subscription_decryption_failed", err, false
	}
	payload, _ := json.Marshal(map[string]string{"title": "TeamMind 新通知", "body": "你有一条新的通知，请登录查看。", "url": strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/") + "/teammind/notifications"})
	status, err := sendWebPush(ctx, sub, payload)
	if status == http.StatusGone || status == http.StatusNotFound {
		return "suppressed", "subscription_expired", nil, true
	}
	if err != nil {
		return "failed", "push_unavailable", err, false
	}
	if status < 200 || status >= 300 {
		return "failed", fmt.Sprintf("push_http_%d", status), errors.New("push endpoint rejected delivery"), false
	}
	return "delivered", "", nil, false
}

func classifyDeliveryError(channel string, err error) string {
	if channel == "email" {
		if !notificationEmailConfigured() {
			return "channel_not_configured"
		}
		return "email_unavailable"
	}
	if strings.Contains(err.Error(), "NOTIFICATION_ENCRYPTION_KEY") {
		return "encryption_key_unavailable"
	}
	if channel == "native_push" {
		return "native_push_unavailable"
	}
	return "push_unavailable"
}
