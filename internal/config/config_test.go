package config

import (
	"os"
	"testing"
)

func TestLoadParsesFileAndEnv(t *testing.T) {
	os.Setenv("BINANCE_API_KEY", "k")
	os.Setenv("BINANCE_API_SECRET", "s")
	defer os.Unsetenv("BINANCE_API_KEY")
	defer os.Unsetenv("BINANCE_API_SECRET")

	c, err := Load("../../testdata/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mode != "paper" || len(c.Symbols) != 1 || c.Symbols[0] != "BTCUSDT" {
		t.Fatalf("bad parse: %+v", c)
	}
	if c.Risk.MaxOpenPositions != 2 || c.Risk.TPRewardMult != 1.6 {
		t.Fatalf("bad risk parse: %+v", c.Risk)
	}
	if c.Secrets.BinanceAPIKey != "k" {
		t.Fatalf("env secret not loaded: %+v", c.Secrets)
	}
	if len(c.Strategies) != 1 || c.Strategies[0].Name != "ema_cross_trend" {
		t.Fatalf("bad strategies: %+v", c.Strategies)
	}
}

func TestLoadRejectsBadMode(t *testing.T) {
	if _, err := Load("../../testdata/missing.yaml"); err == nil {
		t.Fatal("expected error on missing file")
	}
}
