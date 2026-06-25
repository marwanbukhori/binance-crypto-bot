package marketdata

import (
	"encoding/json"
	"strconv"

	"tradebot/internal/domain"
)

type klineMsg struct {
	Event    string `json:"e"`
	EventTime int64  `json:"E"`
	Sym      string `json:"s"`
	K        struct {
		T  int64  `json:"t"`
		TT int64  `json:"T"`
		I  string `json:"i"`
		O  string `json:"o"`
		C  string `json:"c"`
		H  string `json:"h"`
		L  string `json:"l"`
		V  string `json:"v"`
		X  bool   `json:"x"`
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
