package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type sendError struct {
	message   string
	retry     bool
	uncertain bool
}

func (e *sendError) Error() string { return e.message }

func (s *Store) send(ctx context.Context, ch Channel, secret, event, title, body string) error {
	switch ch.Kind {
	case "email":
		return s.sendEmail(ch, secret, title, body)
	case "ntfy":
		return s.sendNtfy(ctx, ch, secret, title, body)
	case "gotify":
		return s.sendGotify(ctx, ch, secret, title, body)
	case "pushover":
		return s.sendPushover(ctx, ch, secret, title, body)
	case "telegram":
		return s.sendTelegram(ctx, ch, secret, title, body)
	case "discord":
		return s.sendDiscord(ctx, ch, secret, title, body)
	case "webhook":
		return s.sendWebhook(ctx, ch, secret, event, title, body)
	default:
		return &sendError{message: "The notification provider is not supported."}
	}
}

func (s *Store) sendNtfy(ctx context.Context, ch Channel, secret, title, body string) error {
	u, err := safeURL(ch.Config["url"], false)
	if err != nil {
		return &sendError{message: "The ntfy URL is invalid."}
	}
	headers := map[string]string{"Title": sanitizeHeader(title), "Tags": "reelscarey", "Priority": "default"}
	if secret != "" {
		headers["Authorization"] = "Bearer " + secret
	}
	return s.postRaw(ctx, u.String(), headers, []byte(title+"\n"+body))
}

func (s *Store) sendGotify(ctx context.Context, ch Channel, secret, title, body string) error {
	u, err := safeURL(ch.Config["url"], false)
	if err != nil {
		return &sendError{message: "The Gotify URL is invalid."}
	}
	endpoint := strings.TrimRight(u.String(), "/") + "/message"
	return s.postJSON(ctx, endpoint, map[string]string{"X-Gotify-Token": secret}, map[string]any{"title": title, "message": body, "priority": 5})
}

func (s *Store) sendPushover(ctx context.Context, ch Channel, secret, title, body string) error {
	if secret == "" {
		return &sendError{message: "A Pushover application token is required."}
	}
	form := url.Values{"token": {secret}, "user": {ch.Config["user"]}, "title": {title}, "message": {body}}
	return s.postForm(ctx, "https://api.pushover.net/1/messages.json", form)
}

func (s *Store) sendTelegram(ctx context.Context, ch Channel, secret, title, body string) error {
	if secret == "" {
		return &sendError{message: "A Telegram bot token is required."}
	}
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(secret) + "/sendMessage"
	return s.postJSON(ctx, endpoint, nil, map[string]any{
		"chat_id": ch.Config["chat_id"], "text": title + "\n" + body, "disable_web_page_preview": true,
	})
}

func (s *Store) sendDiscord(ctx context.Context, ch Channel, secret, title, body string) error {
	if secret == "" || !isDiscordHost(secret) {
		return &sendError{message: "The Discord webhook is missing or invalid."}
	}
	return s.postJSON(ctx, secret, nil, map[string]any{
		"content": title + "\n" + body, "allowed_mentions": map[string]any{"parse": []string{}},
	})
}

func (s *Store) sendWebhook(ctx context.Context, ch Channel, secret, event, title, body string) error {
	payload, err := json.Marshal(map[string]string{"event": event, "title": title, "body": body})
	if err != nil {
		return &sendError{message: "The webhook payload could not be prepared."}
	}
	headers := map[string]string{}
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(payload)
		headers["X-ReelScarey-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	return s.postRawWith(ctx, ch.Config["url"], headers, payload, "application/json")
}

func (s *Store) postJSON(ctx context.Context, rawURL string, headers map[string]string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return &sendError{message: "The notification payload could not be prepared."}
	}
	return s.postRawWith(ctx, rawURL, headers, body, "application/json")
}

func (s *Store) postRaw(ctx context.Context, rawURL string, headers map[string]string, body []byte) error {
	return s.postRawWith(ctx, rawURL, headers, body, "text/plain; charset=utf-8")
}

func (s *Store) postForm(ctx context.Context, rawURL string, form url.Values) error {
	return s.postRawWith(ctx, rawURL, nil, []byte(form.Encode()), "application/x-www-form-urlencoded")
}

func (s *Store) postRawWith(ctx context.Context, rawURL string, headers map[string]string, body []byte, contentType string) error {
	u, err := safeURL(rawURL, false)
	if err != nil {
		return &sendError{message: "The notification URL is invalid."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return &sendError{message: "The notification request could not be prepared."}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ReelScarey")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return &sendError{message: "The provider could not be reached; delivery is uncertain.", uncertain: true}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	case response.StatusCode == http.StatusTooManyRequests:
		return &sendError{message: "The provider rate-limited the notification.", retry: true}
	case response.StatusCode >= 500:
		return &sendError{message: fmt.Sprintf("The provider returned HTTP %d.", response.StatusCode), retry: true}
	default:
		return &sendError{message: fmt.Sprintf("The provider rejected the notification (HTTP %d).", response.StatusCode)}
	}
}

func (s *Store) sendEmail(ch Channel, secret, title, body string) error {
	host := strings.TrimSpace(ch.Config["host"])
	port := strings.TrimSpace(ch.Config["port"])
	if port == "" {
		port = "587"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return &sendError{message: "The SMTP port is invalid."}
	}
	from, err := mail.ParseAddress(ch.Config["from"])
	if err != nil {
		return &sendError{message: "The sender address is invalid."}
	}
	recipients, err := mail.ParseAddressList(ch.Config["to"])
	if err != nil || len(recipients) == 0 {
		return &sendError{message: "At least one recipient address is invalid."}
	}
	to := make([]string, 0, len(recipients))
	for _, address := range recipients {
		to = append(to, address.Address)
	}
	var auth smtp.Auth
	if user := ch.Config["username"]; user != "" {
		auth = smtp.PlainAuth("", user, secret, host)
	}
	message := "From: " + from.String() + "\r\nTo: " + strings.Join(to, ", ") + "\r\nSubject: " + sanitizeHeader(title) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n"
	addr := net.JoinHostPort(host, port)
	implicitTLS := ch.Config["use_tls"] == "true"
	var conn net.Conn
	if implicitTLS {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.DialTimeout("tcp", addr, 15*time.Second)
	}
	if err != nil {
		return &sendError{message: "The SMTP server could not be reached.", retry: true}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return &sendError{message: "The SMTP greeting failed.", retry: true}
	}
	defer client.Close()
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return &sendError{message: "The SMTP server refused STARTTLS.", retry: true}
			}
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return &sendError{message: "The SMTP server rejected the credentials.", retry: true}
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return &sendError{message: "The SMTP server rejected the sender.", retry: true}
	}
	for _, address := range to {
		if err := client.Rcpt(address); err != nil {
			return &sendError{message: "The SMTP server rejected a recipient.", retry: true}
		}
	}
	writer, err := client.Data()
	if err != nil {
		return &sendError{message: "The SMTP server rejected the message.", retry: true}
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return &sendError{message: "The message could not be written; delivery is uncertain.", uncertain: true}
	}
	if err := writer.Close(); err != nil {
		return &sendError{message: "The SMTP result is uncertain.", uncertain: true}
	}
	return nil
}

func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return truncateUTF8(value, 180)
}
