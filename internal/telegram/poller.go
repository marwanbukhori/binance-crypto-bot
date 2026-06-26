package telegram

import (
	"context"

	"tradebot/internal/control"
)

type StatusFunc func() string

// Poll long-polls Telegram and applies commands/approvals to the controller.
func Poll(ctx context.Context, c *Client, ctrl *control.Controller, status, positions StatusFunc) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := c.GetUpdates(offset, 25)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			cmd, dec := Classify(u)
			switch {
			case dec != nil:
				if dec.Approve {
					ctrl.Approve(dec.Token)
				} else {
					ctrl.Reject(dec.Token)
				}
				_ = c.AnswerCallback(dec.CallbackID)
			case cmd != nil:
				switch cmd.Name {
				case "pause":
					ctrl.Pause()
					_ = c.SendMessage(cmd.ChatID, "⏸ paused — no new entries")
				case "resume":
					ctrl.Resume()
					_ = c.SendMessage(cmd.ChatID, "▶️ resumed")
				case "kill":
					ctrl.RequestKill()
					_ = c.SendMessage(cmd.ChatID, "🛑 kill-switch requested")
				case "status":
					_ = c.SendMessage(cmd.ChatID, status())
				case "positions":
					_ = c.SendMessage(cmd.ChatID, positions())
				}
			}
		}
	}
}
