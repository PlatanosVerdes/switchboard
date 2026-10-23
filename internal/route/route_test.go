package route

import (
	"encoding/json"
	"testing"
)

func table(t *testing.T) Table {
	t.Helper()
	tab, err := Parse("wallapop_=http://wallapop:8000/, tv_=http://pixasso:8080", "tv_")
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestFor(t *testing.T) {
	tab := table(t)
	cases := []struct {
		name   string
		update string
		want   string
		ok     bool
	}{
		{"command", `{"message":{"text":"/wallapop_estado"}}`, "http://wallapop:8000", true},
		{"command with bot suffix and args", `{"message":{"text":"/TV_rotate@PlatanosServicesBot 30"}}`, "http://pixasso:8080", true},
		{"photo goes to the fallback", `{"message":{"photo":[{}]}}`, "http://pixasso:8080", true},
		{"command in a photo caption", `{"message":{"caption":"/wallapop_estado"}}`, "http://wallapop:8000", true},
		{"button", `{"callback_query":{"data":"tv_show:MY_F1"}}`, "http://pixasso:8080", true},
		{"unknown command", `{"message":{"text":"/start"}}`, "", false},
		{"unknown button", `{"callback_query":{"data":"other:1"}}`, "", false},
		{"other update type", `{"edited_message":{"text":"/tv_list"}}`, "", false},
		{"broken json", `{`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := tab.For(json.RawMessage(c.update))
			if got != c.want || ok != c.ok {
				t.Fatalf("For(%s) = %q, %v; want %q, %v", c.update, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestNoFallbackDropsPlainMessages(t *testing.T) {
	tab, err := Parse("tv_=http://pixasso:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tab.For(json.RawMessage(`{"message":{"text":"hola"}}`)); ok {
		t.Fatal("a plain message was routed with no fallback")
	}
}

func TestParseRejects(t *testing.T) {
	for _, c := range []struct{ routes, fallback string }{
		{"", ""},
		{"tv_", ""},
		{"=http://x", ""},
		{"tv_=http://x", "wallapop_"},
	} {
		if _, err := Parse(c.routes, c.fallback); err == nil {
			t.Errorf("Parse(%q, %q) accepted it", c.routes, c.fallback)
		}
	}
}
