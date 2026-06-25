package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tradebot/internal/backtest"
	"tradebot/internal/config"
	"tradebot/internal/marketdata"
	"tradebot/internal/risk"
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
	fmt.Printf("tradebot %s — mode=%s symbols=%v (run `bot backtest` to validate a strategy)\n",
		version.Version, cfg.Mode, cfg.Symbols)
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
