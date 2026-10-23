// Package route decides which service an update belongs to.
package route

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Route struct {
	// Prefix is what the service's commands and button data start with: "wallapop_".
	Prefix string
	URL    string
}

type Table struct {
	Routes []Route
	// Fallback takes what carries no command, like a photo sent on its own.
	Fallback string
}

// Parse reads "wallapop_=http://wallapop-catalogue:8000,tv_=http://pixasso:8080".
func Parse(routes, fallback string) (Table, error) {
	var t Table
	for _, entry := range strings.Split(routes, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, url, ok := strings.Cut(entry, "=")
		if !ok || prefix == "" || url == "" {
			return t, fmt.Errorf("route %q is not prefix=url", entry)
		}
		t.Routes = append(t.Routes, Route{Prefix: strings.ToLower(prefix), URL: strings.TrimRight(url, "/")})
	}
	if len(t.Routes) == 0 {
		return t, fmt.Errorf("no routes")
	}
	if fallback != "" {
		r, ok := t.byPrefix(strings.ToLower(fallback))
		if !ok {
			return t, fmt.Errorf("fallback %q is not one of the routes", fallback)
		}
		t.Fallback = r.URL
	}
	return t, nil
}

func (t Table) byPrefix(prefix string) (Route, bool) {
	for _, r := range t.Routes {
		if r.Prefix == prefix {
			return r, true
		}
	}
	return Route{}, false
}

func (t Table) match(name string) (string, bool) {
	for _, r := range t.Routes {
		if strings.HasPrefix(name, r.Prefix) {
			return r.URL, true
		}
	}
	return "", false
}

type update struct {
	Message *struct {
		Text    string `json:"text"`
		Caption string `json:"caption"`
	} `json:"message"`
	CallbackQuery *struct {
		Data string `json:"data"`
	} `json:"callback_query"`
}

// For answers where an update goes; false means nobody wants it.
func (t Table) For(raw json.RawMessage) (string, bool) {
	var u update
	if err := json.Unmarshal(raw, &u); err != nil {
		return "", false
	}
	switch {
	case u.CallbackQuery != nil:
		return t.match(u.CallbackQuery.Data)
	case u.Message != nil:
		text := u.Message.Text
		if text == "" {
			text = u.Message.Caption
		}
		if name, ok := command(text); ok {
			return t.match(name)
		}
		return t.Fallback, t.Fallback != ""
	}
	return "", false
}

// command drops the slash and the @bot suffix group chats add.
func command(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	first, _, _ := strings.Cut(text, " ")
	name, _, _ := strings.Cut(first[1:], "@")
	return strings.ToLower(name), true
}
