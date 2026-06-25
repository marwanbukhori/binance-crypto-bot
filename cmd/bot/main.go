package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tradebot/internal/config"
	"tradebot/internal/marketdata"
	"tradebot/internal/version"
)

func main() {
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
	if len(cfg.Symbols) == 0 {
		log.Fatal("no symbols configured")
	}
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	cs, err := client.Klines(cfg.Symbols[0], "1h", 5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "klines fetch failed (offline is OK in dev): %v\n", err)
		return
	}
	fmt.Printf("tradebot %s — fetched %d candles for %s; last close=%.2f\n",
		version.Version, len(cs), cfg.Symbols[0], cs[len(cs)-1].Close)
}
