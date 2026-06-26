package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"tradebot/internal/backtest"
	"tradebot/internal/config"
	"tradebot/internal/engine"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
	"tradebot/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backtest" {
		runBacktest(os.Args[2:])
		return
	}
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()
	if len(flag.Args()) > 0 && flag.Arg(0) == "version" {
		fmt.Printf("tradebot %s\n", version.Version)
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.Mode == "paper" {
		runPaper(cfg)
		return
	}
	fmt.Printf("tradebot %s — mode=%s symbols=%v (run `bot backtest` to validate a strategy)\n",
		version.Version, cfg.Mode, cfg.Symbols)
}

func runPaper(cfg config.Config) {
	interval := "1h"
	if len(cfg.Strategies) > 0 && cfg.Strategies[0].Timeframe != "" {
		interval = cfg.Strategies[0].Timeframe
	}
	st, err := store.Open("tradebot.db")
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	buf := marketdata.NewBuffer(500)
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	for _, sym := range cfg.Symbols {
		cs, err := client.Klines(sym, interval, 500)
		if err != nil {
			log.Printf("warmup %s failed: %v", sym, err)
			continue
		}
		buf.Seed(sym, cs)
		log.Printf("warmed %s with %d candles", sym, len(cs))
	}
	gate := risk.NewGate(cfg.Risk, cfg.Risk.FeeModel.Majors/100)
	guard := risk.NewGuard(cfg.Risk, 0)
	if killed, _ := st.LoadKillState(); killed {
		guard.Kill()
		log.Print("kill-switch is ACTIVE from a prior session — entries blocked until reset")
	}
	cool := risk.NewCooldown(cfg.Risk.PostLossCooldownCandles)
	pf := portfolio.New(10000) // paper starting balance
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, interval, nil)} }
	feeSide := cfg.Risk.FeeModel.Majors / 2 / 100
	l := engine.NewLive(cfg.Symbols, mk, gate, guard, cool, execution.NewSimulated(feeSide), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, buf)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stream := marketdata.NewWSStream(cfg.Exchange.Testnet, cfg.Symbols, interval)
	defer stream.Close()
	log.Printf("paper trading live on %v (%s) — Ctrl-C to stop", cfg.Symbols, interval)
	if err := l.Run(ctx, stream); err != nil && err != context.Canceled {
		log.Printf("live run ended: %v", err)
	}
}

func runBacktest(args []string) {
	fs := flag.NewFlagSet("backtest", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "config path")
	symbol := fs.String("symbol", "BTCUSDT", "symbol")
	interval := fs.String("interval", "1h", "candle interval")
	bars := fs.Int("bars", 1000, "number of candles")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	candles, err := client.Klines(*symbol, *interval, *bars)
	if err != nil {
		log.Fatalf("klines: %v", err)
	}
	g := risk.NewGate(cfg.Risk, cfg.Risk.FeeModel.Majors/100)
	s := strategy.NewEMACross(*symbol, *interval, nil)
	filt := risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}

	rep := backtest.Run(s, g, filt, candles, 10000, cfg.Risk.FeeModel.Majors/2/100)
	fmt.Printf("=== Backtest %s %s (%d candles) ===\n", *symbol, *interval, len(candles))
	fmt.Printf("trades=%d winRate=%.1f%% netPnL=%.2f expectancy=%.4f maxDD=%.2f%% finalEquity=%.2f\n",
		rep.NumTrades, rep.WinRate*100, rep.NetPnL, rep.Expectancy, rep.MaxDrawdownPct, rep.FinalEquity)
	fmt.Println("--- fee sensitivity (round-trip) ---")
	for _, r := range backtest.Sweep(s, cfg.Risk, filt, candles, 10000, []float64{0.20, 0.30, 0.45}) {
		mark := "OK"
		if !r.Profitable {
			mark = "UNPROFITABLE"
		}
		fmt.Printf("fee %.2f%%: netPnL=%.2f winRate=%.1f%% -> %s\n", r.FeeRoundTripPct, r.NetPnL, r.WinRate*100, mark)
	}
}
