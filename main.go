package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	bot, err := NewBot(config)
	if err != nil {
		log.Fatalf("Error creating bot: %v", err)
	}

	// Create context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		cancel()
	}()

	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Error running bot: %v", err)
	}

	log.Println("Bot stopped gracefully")
}
