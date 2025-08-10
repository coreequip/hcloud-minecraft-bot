package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	ServerName = "mc-one"
)

type Bot struct {
	api     *tgbotapi.BotAPI
	config  *Config
	hetzner *HetznerManager
	porkbun *PorkbunDNSManager

	// Protected by mutex
	mu                  sync.RWMutex
	currentServer       *ServerInfo
	messageCache        map[int]string // MessageID -> Last content
	autoShutdown        bool
	shutdownTimer       *time.Timer
	warningTimers       []*time.Timer     // Track all warning timers
	autoShutdownMessage *tgbotapi.Message // Track auto-shutdown warning message
}

type ServerInfo struct {
	ID        int64
	Name      string
	IP        string
	IsRunning bool
}

func NewBot(config *Config) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		return nil, err
	}

	bot.Debug = false

	return &Bot{
		api:           bot,
		config:        config,
		hetzner:       NewHetznerManager(config),
		porkbun:       NewPorkbunDNSManager(config.PorkbunAPIKey, config.PorkbunSecretKey, config.PorkbunDomain),
		messageCache:  make(map[int]string),
		autoShutdown:  true, // Default: enabled
		warningTimers: make([]*time.Timer, 0),
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	log.Printf("Bot started: @%s", b.api.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Bot shutting down gracefully...")
			b.cancelAllShutdownTimers()
			return ctx.Err()
		case update := <-updates:
			// Check if bot was added to a new group/chat
			if update.Message != nil && update.Message.NewChatMembers != nil {
				for _, member := range update.Message.NewChatMembers {
					if member.ID == b.api.Self.ID {
						// Bot was added to this chat
						chatTitle := update.Message.Chat.Title
						if chatTitle == "" {
							chatTitle = update.Message.Chat.FirstName + " " + update.Message.Chat.LastName
						}
						b.notifyAdmin(fmt.Sprintf("🤖 Bot wurde zu Gruppe hinzugefügt:\n\n📝 Name: %s\n🆔 ID: %d", chatTitle, update.Message.Chat.ID))
						break
					}
				}
			}

			if update.Message == nil || !update.Message.IsCommand() {
				continue
			}

			if update.Message.Chat.ID != b.config.AllowedGroupID {
				continue
			}

			switch update.Message.Command() {
			case "boot":
				go b.handleBootCommand(ctx, update.Message)
			case "shutdown":
				go b.handleShutdownCommand(update.Message)
			case "autoshutdown":
				go b.handleAutoShutdownCommand(update.Message)
			}
		}
	}
}

func (b *Bot) handleBootCommand(ctx context.Context, message *tgbotapi.Message) {
	log.Printf("[BOOT] Command received from @%s (ID: %d)", message.From.UserName, message.From.ID)

	msg := tgbotapi.NewMessage(message.Chat.ID, "🔍 Checking server status...")
	sentMsg, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[BOOT] Error sending message: %v", err)
		return
	}

	log.Printf("[BOOT] Checking if server '%s' is already running...", ServerName)
	server, err := b.hetzner.FindServerByName(ServerName)
	if err != nil {
		log.Printf("[BOOT] Error checking server: %v", err)
		b.updateMessage(sentMsg, "❌ Error checking server")
		return
	}

	if server != nil {
		if server.Status == "running" {
			log.Printf("[BOOT] Server already running with IP: %s", server.PublicNet.IPv4.IP.String())
			b.updateMessage(sentMsg, "✅ Server is already running! You can play! 🎮\n\nIP: `"+server.PublicNet.IPv4.IP.String()+"`")
			return
		} else if server.Status == "off" {
			log.Printf("[BOOT] Server exists but is stopped. Starting it...")
			b.updateMessage(sentMsg, "🔄 Server existiert bereits. Starte Server...")

			_, _, err := b.hetzner.client.Server.Poweron(context.Background(), server)
			if err != nil {
				log.Printf("[BOOT] Error starting server: %v", err)
				b.updateMessage(sentMsg, "❌ Fehler beim Starten des Servers!")
				return
			}

			// Wait for server to be running
			for i := 0; i < 30; i++ {
				time.Sleep(2 * time.Second)
				updatedServer, _, err := b.hetzner.client.Server.GetByID(context.Background(), server.ID)
				if err == nil && updatedServer.Status == hcloud.ServerStatusRunning {
					log.Printf("[BOOT] Server is running!")
					b.updateMessage(sentMsg, "✅ Server gestartet! Warte auf Minecraft...")
					b.waitForMinecraft(ctx, sentMsg, updatedServer.PublicNet.IPv4.IP.String())
					return
				}
			}
			b.updateMessage(sentMsg, "❌ Timeout beim Starten des Servers")
			return
		}
	}

	log.Printf("[BOOT] Starting server boot process...")
	b.notifyAdmin(fmt.Sprintf("🚀 Server being started by @%s", message.From.UserName))

	b.updateMessage(sentMsg, "🔍 Suche neuesten Snapshot...")

	snapshot, err := b.hetzner.FindLatestSnapshot()
	if err != nil {
		log.Printf("[BOOT] Error: No snapshot found - %v", err)
		b.updateMessage(sentMsg, "❌ No snapshot found!")
		return
	}
	log.Printf("[BOOT] Snapshot found: %s (created: %s)", snapshot.Description, snapshot.Created)

	b.updateMessage(sentMsg, fmt.Sprintf("📦 Snapshot found: %s\n\n⚡ Starting server...", snapshot.Description))

	log.Printf("[BOOT] Creating server from snapshot %s...", snapshot.Description)
	newServer, createAction, err := b.hetzner.CreateServerFromSnapshot(snapshot)
	if err != nil {
		log.Printf("[BOOT] Error creating server: %v", err)
		b.updateMessage(sentMsg, "❌ Error creating server!")
		return
	}

	// Update DNS records
	ipv4 := ""
	ipv6 := ""
	if newServer.PublicNet.IPv4.IP != nil {
		ipv4 = newServer.PublicNet.IPv4.IP.String()
	}
	if newServer.PublicNet.IPv6.IP != nil {
		ipv6 = newServer.PublicNet.IPv6.IP.String()
		// Remove the network prefix from IPv6 (e.g., "2001:db8::1/64" -> "2001:db8::1")
		if idx := strings.Index(ipv6, "/"); idx != -1 {
			ipv6 = ipv6[:idx]
		}
	}

	if ipv4 != "" {
		log.Printf("[BOOT] Updating DNS records: IPv4=%s, IPv6=%s", ipv4, ipv6)
		if err := b.porkbun.UpdateDNS(ipv4, ipv6); err != nil {
			log.Printf("[BOOT] Warning: DNS update failed: %v", err)
			// Continue anyway, DNS update is not critical
		} else {
			log.Printf("[BOOT] DNS records successfully updated")
		}
	}

	progressBar := NewProgressBar(100)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	initialProgress := -1

	for {
		select {
		case <-ticker.C:
			// Get actual API progress
			var apiProgress int
			if createAction != nil {
				currentProgress, err := b.hetzner.GetActionProgress(createAction.ID)
				if err == nil {
					apiProgress = currentProgress

					// Initialize starting progress
					if initialProgress == -1 {
						initialProgress = apiProgress
						log.Printf("[BOOT] Initial API progress: %d%%", initialProgress)
					}

					// Calculate normalized progress (50% to 100% mapped to 0% to 100%)
					normalizedProgress := 0
					if initialProgress < 100 {
						progressRange := 100 - initialProgress
						progressMade := apiProgress - initialProgress
						normalizedProgress = int(float64(progressMade) / float64(progressRange) * 100)
					}

					log.Printf("[BOOT] API progress: %d%%, Normalized: %d%%", apiProgress, normalizedProgress)
					progressBar.Update(normalizedProgress)
				}
			}

			updatedServer, err := b.hetzner.FindServerByName(ServerName)
			if err == nil && updatedServer != nil {
				log.Printf("[BOOT] Server Status: %s", updatedServer.Status)
				if updatedServer.Status == "running" {
					log.Printf("[BOOT] Server is up! IP: %s", updatedServer.PublicNet.IPv4.IP.String())
					progressBar.Update(100)
					b.updateMessage(sentMsg, formatProgressMessage("🚀 Server wird erstellt", progressBar, "Server läuft!"))

					// Wait a moment before starting Minecraft check
					time.Sleep(2 * time.Second)
					b.waitForMinecraft(ctx, sentMsg, updatedServer.PublicNet.IPv4.IP.String())
					return
				}
			}

			b.updateMessage(sentMsg, formatProgressMessage("🚀 Server wird erstellt", progressBar, "Server wird initialisiert..."))
		}
	}
}

func (b *Bot) waitForMinecraft(ctx context.Context, message tgbotapi.Message, serverIP string) {
	log.Printf("[BOOT] Waiting for Minecraft server on %s:25565...", serverIP)

	b.updateMessage(message, "⏳ *Minecraft Server startet...*\n\n_Der Server wird initialisiert. Dies kann einige Minuten dauern._")

	mc := NewMinecraftChecker(serverIP)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[BOOT] waitForMinecraft cancelled due to context")
			return
		case <-ticker.C:
			status, err := mc.GetServerStatus()

			if err == nil && status != nil {
				log.Printf("[BOOT] Minecraft server is reachable! Version: %s", status.VersionName)
				b.updateMessage(message, fmt.Sprintf("✅ *Server ist bereit!* 🎮\n\n*IP:* `%s`\n*Port:* 25565\n*Version:* %s\n\nViel Spaß beim Spielen! 🎯", serverIP, status.VersionName))

				// Start player monitoring if auto-shutdown is enabled
				b.mu.RLock()
				autoShutdownEnabled := b.autoShutdown
				b.mu.RUnlock()

				if autoShutdownEnabled {
					go b.startPlayerMonitoring(ctx, serverIP)
					log.Printf("[BOOT] Auto-shutdown monitoring started")
				}

				return
			}

			elapsed := time.Since(startTime)
			statusMsg := fmt.Sprintf("⏳ *Minecraft Server startet...*\n\n_Warte seit %d Sekunden auf Minecraft Server_", int(elapsed.Seconds()))
			b.updateMessage(message, statusMsg)

			if elapsed > 5*time.Minute {
				log.Printf("[BOOT] Timeout: Minecraft server not reachable after 5 minutes")
				b.updateMessage(message, "❌ Timeout beim Warten auf Minecraft Server")
				return
			}
		}
	}
}

func (b *Bot) handleShutdownCommand(message *tgbotapi.Message) {
	log.Printf("[SHUTDOWN] Command received from @%s (ID: %d)", message.From.UserName, message.From.ID)

	msg := tgbotapi.NewMessage(message.Chat.ID, "🔍 Prüfe Server Status...")
	sentMsg, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[SHUTDOWN] Error sending message: %v", err)
		return
	}

	log.Printf("[SHUTDOWN] Checking if server '%s' is running...", ServerName)
	server, err := b.hetzner.FindServerByName(ServerName)
	if err != nil || server == nil {
		log.Printf("[SHUTDOWN] No server found")
		b.updateMessage(sentMsg, "❌ Kein Server gefunden der heruntergefahren werden könnte!")
		return
	}

	if server.Status != "running" {
		log.Printf("[SHUTDOWN] Server not running (Status: %s)", server.Status)
		b.updateMessage(sentMsg, "❌ Server läuft nicht!")
		return
	}

	b.updateMessage(sentMsg, "🔍 Prüfe ob Spieler online sind...")

	log.Printf("[SHUTDOWN] Checking player status on %s:25565...", server.PublicNet.IPv4.IP.String())
	mc := NewMinecraftChecker(server.PublicNet.IPv4.IP.String())
	empty, playerCount, err := mc.IsServerEmpty()
	if err != nil {
		log.Printf("[SHUTDOWN] Error checking players: %v", err)
		b.updateMessage(sentMsg, "⚠️ Kann Spielerstatus nicht prüfen. Server wird nicht heruntergefahren.")
		return
	}

	if !empty {
		log.Printf("[SHUTDOWN] Aborted: %d players still online", playerCount)
		b.updateMessage(sentMsg, fmt.Sprintf("❌ Es sind noch %d Spieler online!\n\nServer kann nicht heruntergefahren werden.", playerCount))
		return
	}

	log.Printf("[SHUTDOWN] No players online, starting shutdown process...")
	b.notifyAdmin(fmt.Sprintf("🛑 Server wird heruntergefahren von @%s", message.From.UserName))

	b.updateMessage(sentMsg, "⏹️ Fahre Server herunter...")

	log.Printf("[SHUTDOWN] Shutting down server...")
	shutdownErr := b.hetzner.ShutdownServer(server)
	if shutdownErr != nil {
		log.Printf("[SHUTDOWN] Warning: Error shutting down server: %v", shutdownErr)
		// Continue anyway - snapshot can be created from running server
	}

	b.updateMessage(sentMsg, "📸 Erstelle Snapshot...")

	log.Printf("[SHUTDOWN] Creating snapshot from server...")
	snapshot, err := b.hetzner.CreateSnapshot(server)
	if err != nil {
		log.Printf("[SHUTDOWN] Error creating snapshot: %v", err)
		b.updateMessage(sentMsg, "❌ Fehler beim Erstellen des Snapshots!")
		return
	}

	log.Printf("[SHUTDOWN] Snapshot created: %s", snapshot.Description)
	b.updateMessage(sentMsg, "🗑️ Lösche Server...")

	log.Printf("[SHUTDOWN] Deleting server...")
	err = b.hetzner.DeleteServer(server)
	if err != nil {
		log.Printf("[SHUTDOWN] Error deleting server: %v", err)
		b.updateMessage(sentMsg, "❌ Fehler beim Löschen des Servers!")
		return
	}

	log.Printf("[SHUTDOWN] Deleting old snapshots...")
	err = b.hetzner.DeleteOldSnapshots(1) // Keep only the newest snapshot
	if err != nil {
		log.Printf("[SHUTDOWN] Warning: Error deleting old snapshots: %v", err)
		// Continue anyway, this is not critical
	}

	log.Printf("[SHUTDOWN] Shutdown process completed. Snapshot: %s", snapshot.Description)
	b.updateMessage(sentMsg, fmt.Sprintf("✅ *Server erfolgreich heruntergefahren!*\n\n📸 Snapshot erstellt: `%s`\n💾 Server wurde gelöscht\n🧹 Alte Snapshots wurden aufgeräumt\n\nDer Server kann jederzeit mit `/boot` wieder gestartet werden.", snapshot.Description))
}

func (b *Bot) updateMessage(message tgbotapi.Message, text string) {
	// Check if message content has changed
	b.mu.RLock()
	lastContent, exists := b.messageCache[message.MessageID]
	b.mu.RUnlock()

	if exists && lastContent == text {
		// Skip update if content is the same
		return
	}

	editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, message.MessageID, text)
	editMsg.ParseMode = "Markdown"

	_, err := b.api.Send(editMsg)
	if err != nil {
		log.Printf("Error updating message: %v", err)
	} else {
		// Update cache on successful update
		b.mu.Lock()
		b.messageCache[message.MessageID] = text
		b.mu.Unlock()
	}
}

func (b *Bot) notifyAdmin(text string) {
	log.Printf("[ADMIN] Sending notification: %s", text)
	msg := tgbotapi.NewMessage(b.config.AdminTGID, text)
	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[ADMIN] Error notifying admin: %v", err)
	}
}

func (b *Bot) cancelAllShutdownTimers() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Cancel main shutdown timer
	if b.shutdownTimer != nil {
		b.shutdownTimer.Stop()
		b.shutdownTimer = nil
	}

	// Cancel all warning timers
	for _, timer := range b.warningTimers {
		if timer != nil {
			timer.Stop()
		}
	}
	b.warningTimers = make([]*time.Timer, 0)

	// Clear message reference and update it to show cancellation (only if message exists)
	if b.autoShutdownMessage != nil {
		b.updateMessage(*b.autoShutdownMessage, "✅ *Auto-Shutdown abgebrochen*\n\nSpieler sind wieder online oder Auto-Shutdown wurde deaktiviert.")
		b.autoShutdownMessage = nil
	}
}

func (b *Bot) handleAutoShutdownCommand(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		// Show current status
		b.mu.RLock()
		status := "aktiviert"
		if !b.autoShutdown {
			status = "deaktiviert"
		}
		b.mu.RUnlock()

		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("🤖 Auto-Shutdown ist derzeit *%s*\n\nVerwende:\n`/autoshutdown on` - Aktivieren\n`/autoshutdown off` - Deaktivieren", status))
		msg.ParseMode = "Markdown"
		b.api.Send(msg)
		return
	}

	switch args[1] {
	case "on":
		b.mu.Lock()
		b.autoShutdown = true
		b.mu.Unlock()

		msg := tgbotapi.NewMessage(message.Chat.ID, "✅ Auto-Shutdown wurde *aktiviert*\n\nDer Server wird automatisch heruntergefahren, wenn 10 Minuten lang keine Spieler online sind.")
		msg.ParseMode = "Markdown"
		b.api.Send(msg)
		log.Printf("[AUTOSHUTDOWN] Enabled by @%s", message.From.UserName)

	case "off":
		b.mu.Lock()
		b.autoShutdown = false
		b.mu.Unlock()

		b.cancelAllShutdownTimers()
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Auto-Shutdown wurde *deaktiviert*")
		msg.ParseMode = "Markdown"
		b.api.Send(msg)
		log.Printf("[AUTOSHUTDOWN] Disabled by @%s", message.From.UserName)

	default:
		msg := tgbotapi.NewMessage(message.Chat.ID, "❓ Unbekannter Parameter. Verwende `on` oder `off`")
		b.api.Send(msg)
	}
}

func (b *Bot) startPlayerMonitoring(ctx context.Context, serverIP string) {
	log.Printf("[MONITOR] Starting player monitoring for %s", serverIP)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	emptyStartTime := time.Time{}

	for {
		select {
		case <-ctx.Done():
			log.Printf("[MONITOR] Stopping player monitoring due to context cancellation")
			return
		case <-ticker.C:
			b.mu.RLock()
			autoShutdownEnabled := b.autoShutdown
			b.mu.RUnlock()

			if !autoShutdownEnabled {
				// Clear any existing timers if auto-shutdown was disabled
				if !emptyStartTime.IsZero() {
					emptyStartTime = time.Time{}
					b.cancelAllShutdownTimers()
				}
				continue
			}

			// Check if server still exists
			server, err := b.hetzner.FindServerByName(ServerName)
			if err != nil || server == nil || server.Status != "running" {
				log.Printf("[MONITOR] Server no longer exists or not running, stopping monitoring")
				b.cancelAllShutdownTimers()
				return
			}

			// Check player count
			mc := NewMinecraftChecker(serverIP)
			empty, playerCount, err := mc.IsServerEmpty()
			if err != nil {
				log.Printf("[MONITOR] Error checking players: %v", err)
				emptyStartTime = time.Time{}
				b.cancelAllShutdownTimers()
				continue
			}

			if !empty {
				log.Printf("[MONITOR] %d players online", playerCount)
				if !emptyStartTime.IsZero() {
					log.Printf("[MONITOR] Players back online, shutdown timer reset")
					emptyStartTime = time.Time{}
					b.cancelAllShutdownTimers()
				}
			} else {
				if emptyStartTime.IsZero() {
					emptyStartTime = time.Now()
					log.Printf("[MONITOR] Server is empty, starting 10-minute timer")

					// Cancel any existing timers first
					b.cancelAllShutdownTimers()

					// Schedule shutdown in 10 minutes
					b.mu.Lock()
					b.shutdownTimer = time.AfterFunc(10*time.Minute, func() {
						b.mu.RLock()
						autoShutdownEnabled := b.autoShutdown
						b.mu.RUnlock()

						if autoShutdownEnabled { // Double-check auto-shutdown is still enabled
							b.performAutoShutdown()
						}
					})

					// Schedule warning messages
					warningTimer1 := time.AfterFunc(5*time.Minute, func() {
						b.mu.RLock()
						autoShutdownEnabled := b.autoShutdown
						b.mu.RUnlock()

						if autoShutdownEnabled {
							// Create warning message on first warning
							msg := tgbotapi.NewMessage(b.config.AllowedGroupID, "⚠️ *Auto-Shutdown Warnung*\n\nServer wird in 5 Minuten automatisch heruntergefahren!")
							msg.ParseMode = "Markdown"
							sentMsg, err := b.api.Send(msg)
							if err == nil {
								b.mu.Lock()
								b.autoShutdownMessage = &sentMsg
								b.mu.Unlock()
							}
						}
					})
					warningTimer2 := time.AfterFunc(9*time.Minute, func() {
						b.mu.RLock()
						autoShutdownEnabled := b.autoShutdown
						autoShutdownMsg := b.autoShutdownMessage
						b.mu.RUnlock()

						if autoShutdownEnabled && autoShutdownMsg != nil {
							b.updateMessage(*autoShutdownMsg, "🚨 *Auto-Shutdown Warnung*\n\nServer wird in 1 Minute automatisch heruntergefahren!")
						}
					})

					// Track warning timers
					b.warningTimers = []*time.Timer{warningTimer1, warningTimer2}
					b.mu.Unlock()
				} else {
					elapsed := time.Since(emptyStartTime)
					log.Printf("[MONITOR] Server empty for %.0f minutes", elapsed.Minutes())
				}
			}
		}
	}
}

func (b *Bot) performAutoShutdown() {
	log.Printf("[AUTOSHUTDOWN] Performing automatic shutdown")

	// Use existing message or create new one
	var sentMsg tgbotapi.Message
	b.mu.RLock()
	autoShutdownMsg := b.autoShutdownMessage
	b.mu.RUnlock()

	if autoShutdownMsg != nil {
		b.updateMessage(*autoShutdownMsg, "🤖 *Automatischer Shutdown*\n\nDer Server war 10 Minuten lang leer und wird jetzt heruntergefahren.")
		sentMsg = *autoShutdownMsg
	} else {
		msg := tgbotapi.NewMessage(b.config.AllowedGroupID, "🤖 *Automatischer Shutdown*\n\nDer Server war 10 Minuten lang leer und wird jetzt heruntergefahren.")
		msg.ParseMode = "Markdown"
		sentMsg, _ = b.api.Send(msg)
	}

	// Find server
	server, err := b.hetzner.FindServerByName(ServerName)
	if err != nil || server == nil {
		b.updateMessage(sentMsg, "❌ Server nicht gefunden")
		return
	}

	// Perform shutdown
	b.updateMessage(sentMsg, "⏹️ Fahre Server herunter...")

	log.Printf("[AUTOSHUTDOWN] Shutting down server...")
	shutdownErr := b.hetzner.ShutdownServer(server)
	if shutdownErr != nil {
		log.Printf("[AUTOSHUTDOWN] Warning: Error shutting down server: %v", shutdownErr)
		// Continue anyway - snapshot can be created from running server
	}

	b.updateMessage(sentMsg, "📸 Erstelle Snapshot...")

	snapshot, err := b.hetzner.CreateSnapshot(server)
	if err != nil {
		log.Printf("[AUTOSHUTDOWN] Error creating snapshot: %v", err)
		b.updateMessage(sentMsg, "❌ Fehler beim Erstellen des Snapshots!")
		return
	}
	log.Printf("[AUTOSHUTDOWN] Snapshot created successfully: %s", snapshot.Description)

	b.updateMessage(sentMsg, "🗑️ Lösche Server...")
	log.Printf("[AUTOSHUTDOWN] Starting server deletion...")

	err = b.hetzner.DeleteServer(server)
	if err != nil {
		log.Printf("[AUTOSHUTDOWN] Error deleting server: %v", err)
		b.updateMessage(sentMsg, "❌ Fehler beim Löschen des Servers!")
		return
	}
	log.Printf("[AUTOSHUTDOWN] Server deleted successfully")

	err = b.hetzner.DeleteOldSnapshots(1)
	if err != nil {
		log.Printf("[AUTOSHUTDOWN] Warning: Error deleting old snapshots: %v", err)
	}

	log.Printf("[AUTOSHUTDOWN] Auto-shutdown completed successfully")
	b.updateMessage(sentMsg, fmt.Sprintf("✅ *Auto-Shutdown erfolgreich!*\n\n📸 Snapshot: `%s`\n🤖 Server wurde automatisch heruntergefahren\n\nDer Server kann mit `/boot` wieder gestartet werden.", snapshot.Description))
	b.notifyAdmin("🤖 Server wurde automatisch heruntergefahren (10 Minuten leer)")
}
