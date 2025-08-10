# Minecraft Server Telegram Bot

A Telegram bot for managing a Minecraft server on Hetzner Cloud with automatic cost optimization.

## Features

- **`/boot`** - Starts the Minecraft server from the latest snapshot
- **`/shutdown`** - Creates a snapshot and deletes the server (only if no players are online)
- **`/autoshutdown`** - Toggle automatic shutdown when server is empty (on/off)
- Progress display with Unicode progress bars
- Admin notifications for server actions
- Automatic player check before shutdown
- DNS record updates via Porkbun API
- Automatic server shutdown after 10 minutes of inactivity
- Real-time progress tracking during server creation
- Message caching to prevent Telegram API errors
- Automatic cleanup of old snapshots

## Auto-Shutdown Feature

The bot monitors player activity every minute when auto-shutdown is enabled (default). If the server is empty for 10 minutes, it will:
1. Send a warning at 5 minutes
2. Send a final warning at 1 minute
3. Create a snapshot and shut down the server
4. Clean up old snapshots (keeping only the latest)

If players join during the countdown, the timer resets.

## Installation

### Option 1: Using Docker (Recommended)

1. Pull the latest image:
```bash
docker pull ghcr.io/yourusername/hcloud-minecraft-bot:latest
```

2. Create a `.env` file based on `.env.example`

3. Run with docker-compose:
```bash
docker-compose up -d
```

Or run directly with Docker:
```bash
docker run -d \
  --name hcloud-minecraft-bot \
  --env-file .env \
  --restart unless-stopped \
  ghcr.io/coreequip/hcloud-minecraft-bot:latest
```

### Option 2: Build from Source

1. Install dependencies:
```bash
go mod download
```

2. Set environment variables (see `.env.example`):
- `HETZNER_TOKEN` - Hetzner Cloud API Token
- `TELEGRAM_BOT_TOKEN` - Telegram Bot Token
- `ALLOWED_GROUP_ID` - Telegram Group ID allowed to use the bot
- `ADMIN_TG_ID` - Admin Telegram ID for notifications
- `HETZNER_LOCATION` - (Optional) Server location (default: nbg1)
- `PORKBUN_API_KEY` - Porkbun API key for DNS updates
- `PORKBUN_SECRET_KEY` - Porkbun secret key for DNS updates
- `PORKBUN_DOMAIN` - Domain for DNS updates (e.g., "mc.example.com" or "example.com")

3. Start the bot:
```bash
go run .
```

## Usage

1. Add bot to your Telegram group
2. `/boot` - Starts the server (CAX31 instance)
3. `/shutdown` - Shuts down the server and creates snapshot
4. `/autoshutdown` - Shows current auto-shutdown status
5. `/autoshutdown on` - Enable automatic shutdown (default)
6. `/autoshutdown off` - Disable automatic shutdown

## Technical Details

- Snapshots are saved with prefix `mcone-` and timestamp
- Server will only shut down if no players are online
- The bot always uses the latest available snapshot for booting
- DNS records (A and AAAA) are automatically updated when server starts
- Server creation updates are sent every 5 seconds
- Progress is calculated from actual API progress (normalized from 50-100% to 0-100%)
- Minecraft server startup is displayed as status only (not part of progress)
- Old snapshots are automatically deleted after server shutdown
- DNS updates support both subdomains (e.g., "mc.example.com") and root domains (e.g., "example.com")

## Cost Optimization

This bot helps minimize server costs by:
- Only running the server when players are active
- Automatically shutting down after 10 minutes of inactivity
- Cleaning up old snapshots to reduce storage costs
- Using snapshots for fast server restoration

## Requirements

- Go 1.19+ (for building from source)
- Hetzner Cloud account with API token
- Telegram Bot (created via @BotFather)
- Porkbun account with API access (for DNS updates)
- Docker (for containerized deployment)

## Docker Images

Docker images are automatically built and published to GitHub Container Registry when a new release is tagged:

- `ghcr.io/yourusername/hcloud-minecraft-bot:latest` - Latest release
- `ghcr.io/yourusername/hcloud-minecraft-bot:v1.0.0` - Specific version
- `ghcr.io/yourusername/hcloud-minecraft-bot:1.0` - Major.minor version
- `ghcr.io/yourusername/hcloud-minecraft-bot:1` - Major version only

The images are built as minimal scratch containers (~10MB) and support both `linux/amd64` and `linux/arm64` architectures.