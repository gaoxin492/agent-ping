package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Message struct {
	Title string
	Body  string
}

var httpClient = &http.Client{Timeout: 8 * time.Second}

func sendAll(cfg *Config, m Message) error {
	if len(cfg.Channels) == 0 {
		return errors.New(T(cfg, "no_channel"))
	}
	if cfg.Machine != "" {
		m.Body = strings.TrimSpace(m.Body + "\n" + T(cfg, "machine", cfg.Machine))
	}
	var errs []string
	for _, c := range cfg.Channels {
		if err := send(c, m); err != nil {
			errs = append(errs, c.Type+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func send(c Channel, m Message) error {
	text := strings.TrimSpace(m.Title + "\n" + m.Body)
	switch c.Type {
	case "ntfy":
		hdr := map[string]string{}
		if c.Token != "" {
			hdr["Authorization"] = "Bearer " + c.Token
		}
		return postJSON(strings.TrimRight(or(c.Server, "https://ntfy.sh"), "/"), map[string]any{
			"topic": c.Topic, "title": m.Title, "message": or(m.Body, " "), "priority": 4,
		}, hdr)
	case "bark":
		return postJSON(strings.TrimRight(or(c.Server, "https://api.day.app"), "/")+"/push", map[string]any{
			"device_key": c.Key, "title": m.Title, "body": or(m.Body, " "),
			"group": "agent-ping", "level": "timeSensitive",
		}, nil)
	case "pushover":
		resp, err := httpClient.PostForm("https://api.pushover.net/1/messages.json", url.Values{
			"token": {c.Token}, "user": {c.Key}, "title": {m.Title}, "message": {or(m.Body, " ")},
		})
		return checkResp(resp, err)
	case "telegram":
		return postJSON("https://api.telegram.org/bot"+c.Token+"/sendMessage",
			map[string]any{"chat_id": c.ChatID, "text": text}, nil)
	case "webhook":
		// "text" works for Slack, "content" for Discord, the rest for anything custom.
		return postJSON(c.URL, map[string]any{
			"title": m.Title, "message": m.Body, "text": text, "content": text,
		}, nil)
	}
	return fmt.Errorf("unknown channel type %q", c.Type)
}

func postJSON(u string, body any, hdr map[string]string) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return checkResp(httpClient.Do(req))
}

func checkResp(resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	// Bark answers 200 with {"code":400,...} on errors.
	var r struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(b, &r) == nil && r.Code != nil && *r.Code != 200 {
		return fmt.Errorf("server said: %s", strings.TrimSpace(string(b)))
	}
	return nil
}
