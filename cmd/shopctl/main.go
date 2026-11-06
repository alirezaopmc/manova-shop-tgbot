package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

const Version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "version":
		fmt.Printf("shopctl v%s (Manova Hetzner Cloud Shop Bot)\n", Version)

	case "invite":
		inviteCmd := flag.NewFlagSet("invite", flag.ExitOnError)
		uses := inviteCmd.Int("uses", 1, "Number of uses (0 = unlimited)")
		days := inviteCmd.Int("days", 0, "Validity days (0 = no expiry)")
		_ = inviteCmd.Parse(os.Args[2:])

		cfg, _ := config.LoadFromEnv()
		now := time.Now().UTC()
		var dur *time.Duration
		if *days > 0 {
			d := time.Duration(*days) * 24 * time.Hour
			dur = &d
		}

		inv := invite.New(cfg.AdminTelegramID, *uses, dur, now)
		fmt.Printf("New Invite Generated Successfully:\n")
		fmt.Printf("• Code:     %s\n", inv.Code)
		fmt.Printf("• Max Uses: %d (0 = unlimited)\n", inv.MaxUses)
		fmt.Printf("• DeepLink: /start invite_%s\n", inv.Code)

	case "catalog":
		store := memory.NewStore()
		items, _ := store.Catalog().ListActiveItems(context.Background())
		fmt.Printf("Active Hetzner Cloud Catalog:\n")
		for _, it := range items {
			fmt.Printf("• %s: %d vCPU | %d GB RAM | %d GB NVMe SSD | Monthly Cap: $%0.2f\n",
				it.Name, it.VCPU, it.RAMGB, it.DiskGB, float64(it.MonthlyCapUSD)/1_000_000.0)
		}

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("shopctl - Manova Shop Bot Operations CLI")
	fmt.Println("\nUsage:")
	fmt.Println("  shopctl version             Print version")
	fmt.Println("  shopctl invite [--uses N]   Create onboarding invite")
	fmt.Println("  shopctl catalog             List available cloud server catalog")
}
