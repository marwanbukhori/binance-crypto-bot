package main

import (
	"fmt"

	"tradebot/internal/version"
)

func main() {
	fmt.Printf("tradebot %s\n", version.Version)
}
