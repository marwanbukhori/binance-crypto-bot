package telegram

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetUpdatesParses(t *testing.T) {
	body := `{"ok":true,"result":[
	  {"update_id":10,"message":{"text":"/status","chat":{"id":42}}},
	  {"update_id":11,"callback_query":{"id":"cb1","data":"approve:tok7","message":{"chat":{"id":42}}}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "getUpdates") { t.Errorf("unexpected path %s", r.URL.Path) }
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient("tok").WithHTTP(srv.Client(), srv.URL)
	us, err := c.GetUpdates(0, 0)
	if err != nil { t.Fatalf("getUpdates: %v", err) }
	if len(us) != 2 { t.Fatalf("want 2 updates, got %d", len(us)) }
	if us[0].Message == nil || us[0].Message.Text != "/status" || us[0].Message.Chat.ID != 42 {
		t.Fatalf("bad message update: %+v", us[0])
	}
	if us[1].CallbackQuery == nil || us[1].CallbackQuery.Data != "approve:tok7" {
		t.Fatalf("bad callback update: %+v", us[1])
	}
}

func TestSendMessageHitsAPI(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	c := NewClient("tok").WithHTTP(srv.Client(), srv.URL)
	if err := c.SendMessage(42, "hi"); err != nil { t.Fatalf("send: %v", err) }
	if !strings.Contains(gotPath, "sendMessage") { t.Fatalf("expected sendMessage path, got %s", gotPath) }
}
