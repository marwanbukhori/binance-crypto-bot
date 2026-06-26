package telegram

// Notifier sends outbound messages and approval requests.
type Notifier interface {
	Info(text string) error
	AskApproval(token, text string) error
}

type TelegramNotifier struct {
	c      *Client
	chatID int64
}

func NewTelegramNotifier(c *Client, chatID int64) *TelegramNotifier {
	return &TelegramNotifier{c: c, chatID: chatID}
}

func (n *TelegramNotifier) Info(text string) error { return n.c.SendMessage(n.chatID, text) }

func (n *TelegramNotifier) AskApproval(token, text string) error {
	return n.c.SendButtons(n.chatID, text, [][2]string{
		{"✅ Approve", "approve:" + token},
		{"❌ Reject", "reject:" + token},
	})
}

type NoopNotifier struct{}

func (NoopNotifier) Info(string) error                 { return nil }
func (NoopNotifier) AskApproval(string, string) error { return nil }
