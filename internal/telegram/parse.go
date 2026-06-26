package telegram

import "strings"

type Command struct {
	Name   string
	ChatID int64
}

type ApprovalDecision struct {
	Token      string
	Approve    bool
	CallbackID string
	ChatID     int64
}

func Classify(u Update) (*Command, *ApprovalDecision) {
	if u.CallbackQuery != nil {
		d := u.CallbackQuery.Data
		if t, ok := strings.CutPrefix(d, "approve:"); ok {
			return nil, &ApprovalDecision{Token: t, Approve: true, CallbackID: u.CallbackQuery.ID, ChatID: u.CallbackQuery.Message.Chat.ID}
		}
		if t, ok := strings.CutPrefix(d, "reject:"); ok {
			return nil, &ApprovalDecision{Token: t, Approve: false, CallbackID: u.CallbackQuery.ID, ChatID: u.CallbackQuery.Message.Chat.ID}
		}
		return nil, nil
	}
	if u.Message != nil && strings.HasPrefix(u.Message.Text, "/") {
		name := strings.ToLower(strings.TrimPrefix(strings.Fields(u.Message.Text)[0], "/"))
		return &Command{Name: name, ChatID: u.Message.Chat.ID}, nil
	}
	return nil, nil
}
