package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type StrategyCfg struct {
	Name      string             `yaml:"name"`
	Enabled   bool               `yaml:"enabled"`
	Timeframe string             `yaml:"timeframe"`
	Params    map[string]float64 `yaml:"params"`
}

type FeeModel struct {
	Majors      float64 `yaml:"majors"`
	Alts        float64 `yaml:"alts"`
	BNBDiscount bool    `yaml:"bnb_discount"`
}

type RiskCfg struct {
	MaxPctPerTrade          float64  `yaml:"max_pct_per_trade"`
	RiskPerTradePct         float64  `yaml:"risk_per_trade_pct"`
	MaxOpenPositions        int      `yaml:"max_open_positions"`
	PortfolioMaxDeployedPct float64  `yaml:"portfolio_max_deployed_pct"`
	DailyLossLimitPct       float64  `yaml:"daily_loss_limit_pct"`
	HardFlattenDrawdownPct  float64  `yaml:"hard_flatten_drawdown_pct"`
	WeeklyLossLimitPct      float64  `yaml:"weekly_loss_limit_pct"`
	PostLossCooldownCandles int      `yaml:"post_loss_cooldown_candles"`
	TPRewardMult            float64  `yaml:"tp_reward_mult"`
	TrailingStopPct         float64  `yaml:"trailing_stop_pct"`
	FeeModel                FeeModel `yaml:"fee_model"`
}

type Secrets struct {
	BinanceAPIKey, BinanceAPISecret  string
	TelegramBotToken, TelegramChatID string
	DashboardToken                   string
}

type Config struct {
	Mode       string        `yaml:"mode"`
	Exchange   struct{ Testnet bool `yaml:"testnet"` } `yaml:"exchange"`
	Symbols    []string      `yaml:"symbols"`
	Strategies []StrategyCfg `yaml:"strategies"`
	Risk       RiskCfg       `yaml:"risk"`
	Secrets    Secrets       `yaml:"-"`
}

// Load reads YAML config and overlays secrets from environment variables.
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	switch c.Mode {
	case "backtest", "paper", "live":
	default:
		return c, fmt.Errorf("invalid mode %q (want backtest|paper|live)", c.Mode)
	}
	c.Secrets = Secrets{
		BinanceAPIKey:    os.Getenv("BINANCE_API_KEY"),
		BinanceAPISecret: os.Getenv("BINANCE_API_SECRET"),
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		DashboardToken:   os.Getenv("DASHBOARD_TOKEN"),
	}
	return c, nil
}
