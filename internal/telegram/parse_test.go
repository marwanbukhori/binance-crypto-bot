package telegram

import "testing"

func TestClassifyCommand(t *testing.T) {
	cmd, dec := Classify(Update{Message: &Message{Text: "/Status", Chat: Chat{ID: 7}}})
	if dec != nil || cmd == nil || cmd.Name != "status" || cmd.ChatID != 7 {
		t.Fatalf("bad command parse: %+v %+v", cmd, dec)
	}
}

func TestClassifyApproval(t *testing.T) {
	cmd, dec := Classify(Update{CallbackQuery: &CallbackQuery{ID: "cb", Data: "approve:tok7", Message: Message{Chat: Chat{ID: 7}}}})
	if cmd != nil || dec == nil || dec.Token != "tok7" || !dec.Approve || dec.CallbackID != "cb" {
		t.Fatalf("bad approval parse: %+v %+v", cmd, dec)
	}
	_, dec2 := Classify(Update{CallbackQuery: &CallbackQuery{Data: "reject:tok7"}})
	if dec2 == nil || dec2.Approve {
		t.Fatalf("reject must set Approve=false: %+v", dec2)
	}
}

func TestClassifyIgnoresPlainText(t *testing.T) {
	cmd, dec := Classify(Update{Message: &Message{Text: "hello"}})
	if cmd != nil || dec != nil { t.Fatal("plain text must be ignored") }
}
