package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"tradebot/internal/domain"
)

type klineMsg struct {
	Event    string `json:"e"`
	EventTime int64  `json:"E"`
	Sym      string `json:"s"`
	K        struct {
		T  int64  `json:"t"` // open time
		TT int64  `json:"T"` // close time
		I  string `json:"i"`
		O  string `json:"o"`
		C  string `json:"c"`
		H  string `json:"h"`
		L  string `json:"l"` // low price
		V  string `json:"v"` // base volume
		X  bool   `json:"x"`
		// Binance kline objects also carry case-colliding keys. Go's JSON matching is
		// case-insensitive, so without these exact-case fields the numeric "L"
		// (last trade id) hijacks "l" (low) and errors, and "V" overwrites "v".
		// Declaring them makes the exact-case match win and keeps price fields intact.
		FirstID int64  `json:"f"` // first trade id
		LastID  int64  `json:"L"` // last trade id (number — must not land in L/low)
		NumTr   int64  `json:"n"` // number of trades
		TakerV  string `json:"V"` // taker buy base volume (must not overwrite v)
		TakerQ  string `json:"Q"` // taker buy quote volume
	} `json:"k"`
}

// parseKlineEvent decodes a Binance kline WS frame; closed is true only when k.x is set.
func parseKlineEvent(raw []byte) (domain.Candle, bool, error) {
	var m klineMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return domain.Candle{}, false, err
	}
	if m.Event != "kline" {
		return domain.Candle{}, false, nil
	}
	f := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	c := domain.Candle{
		Symbol: m.Sym, Timeframe: m.K.I,
		OpenTime: m.K.T, CloseTime: m.K.TT,
		Open: f(m.K.O), High: f(m.K.H), Low: f(m.K.L), Close: f(m.K.C), Volume: f(m.K.V),
		Closed: m.K.X,
	}
	return c, m.K.X, nil
}

type Stream interface {
	Candles() <-chan domain.Candle
	Close() error
}

// ChanStream adapts a plain channel to Stream (used for tests and the sim path).
type ChanStream struct{ ch chan domain.Candle }

func NewChanStream(ch chan domain.Candle) *ChanStream { return &ChanStream{ch: ch} }
func (c *ChanStream) Candles() <-chan domain.Candle   { return c.ch }
func (c *ChanStream) Close() error                    { return nil }

func streamURL(testnet bool, symbols []string, interval string) string {
	host := "wss://stream.binance.com:9443"
	if testnet {
		host = "wss://testnet.binance.vision"
	}
	parts := make([]string, len(symbols))
	for i, s := range symbols {
		parts[i] = strings.ToLower(s) + "@kline_" + interval
	}
	return fmt.Sprintf("%s/stream?streams=%s", host, strings.Join(parts, "/"))
}

type WSStream struct {
	url     string
	out     chan domain.Candle
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	conn    *websocket.Conn
}

func NewWSStream(testnet bool, symbols []string, interval string) *WSStream {
	w := &WSStream{url: streamURL(testnet, symbols, interval), out: make(chan domain.Candle, 64), done: make(chan struct{})}
	go w.run()
	return w
}

func (w *WSStream) Candles() <-chan domain.Candle { return w.out }

func (w *WSStream) setConn(c *websocket.Conn) {
	w.mu.Lock()
	w.conn = c
	w.mu.Unlock()
}

func (w *WSStream) Close() error {
	w.once.Do(func() {
		close(w.done)
		w.mu.Lock()
		if w.conn != nil {
			w.conn.Close()
		}
		w.mu.Unlock()
	})
	return nil
}

// combined-stream frames wrap the payload as {"stream":"...","data":{...}}.
type combinedFrame struct {
	Data json.RawMessage `json:"data"`
}

func (w *WSStream) run() {
	backoff := time.Second
	for {
		select {
		case <-w.done:
			return
		default:
		}
		conn, _, err := websocket.DefaultDialer.Dial(w.url, nil)
		if err != nil {
			select {
			case <-time.After(backoff):
			case <-w.done:
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		w.setConn(conn)
		backoff = time.Second
		for {
			select {
			case <-w.done:
				w.setConn(nil)
				conn.Close()
				return
			default:
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				w.setConn(nil)
				conn.Close()
				break // reconnect
			}
			payload := msg
			var cf combinedFrame
			if json.Unmarshal(msg, &cf) == nil && len(cf.Data) > 0 {
				payload = cf.Data
			}
			if c, closed, perr := parseKlineEvent(payload); perr == nil && closed {
				select {
				case w.out <- c:
				case <-w.done:
					w.setConn(nil)
					conn.Close()
					return
				}
			}
		}
	}
}
