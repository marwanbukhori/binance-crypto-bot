package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Chat struct{ ID int64 `json:"id"` }
type Message struct {
	Text string `json:"text"`
	Chat Chat   `json:"chat"`
}
type CallbackQuery struct {
	ID      string  `json:"id"`
	Data    string  `json:"data"`
	Message Message `json:"message"`
}
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Client struct {
	token string
	base  string
	http  *http.Client
}

func NewClient(token string) *Client {
	return &Client{token: token, base: "https://api.telegram.org", http: &http.Client{Timeout: 70 * time.Second}}
}

func (c *Client) WithHTTP(h *http.Client, base string) *Client {
	c.http = h
	c.base = base
	return c
}

func (c *Client) method(name string) string {
	return fmt.Sprintf("%s/bot%s/%s", c.base, c.token, name)
}

func (c *Client) post(method string, form url.Values) ([]byte, error) {
	resp, err := c.http.PostForm(c.method(method), form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: status %d", method, resp.StatusCode)
	}
	var buf [1 << 16]byte
	n, _ := resp.Body.Read(buf[:])
	return buf[:n], nil
}

func (c *Client) SendMessage(chatID int64, text string) error {
	_, err := c.post("sendMessage", url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "text": {text}})
	return err
}

func (c *Client) SendButtons(chatID int64, text string, buttons [][2]string) error {
	rows := make([][]map[string]string, len(buttons))
	for i, b := range buttons {
		rows[i] = []map[string]string{{"text": b[0], "callback_data": b[1]}}
	}
	markup, _ := json.Marshal(map[string]any{"inline_keyboard": rows})
	_, err := c.post("sendMessage", url.Values{
		"chat_id":      {strconv.FormatInt(chatID, 10)},
		"text":         {text},
		"reply_markup": {string(markup)},
	})
	return err
}

func (c *Client) AnswerCallback(id string) error {
	_, err := c.post("answerCallbackQuery", url.Values{"callback_query_id": {id}})
	return err
}

func (c *Client) GetUpdates(offset int64, timeoutSec int) ([]Update, error) {
	u := fmt.Sprintf("%s?offset=%d&timeout=%d", c.method("getUpdates"), offset, timeoutSec)
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Result, nil
}
