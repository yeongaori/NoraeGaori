# NoraeGaori

A feature-rich & high-quality audio Discord music bot written in Go.

## Features

- **High-Quality Audio Streaming** from YouTube via yt-dlp
- **Persistent Queue**
- **SponsorBlock**
- **Live Stream Support**
- **Queue Management** — move, swap, skip-to, remove by range
- **Per-Guild Settings** — volume, repeat, prefix, normalization
- **Auto-Pause** when voice channel empties
- **Slash Commands & Prefix Commands**
- **Multi-Language Support**
- **Admin Commands**
- **Hot-Reload Config**

## Prerequisites

- **Go** 1.25+
- **FFmpeg** (`sudo apt install ffmpeg`)
- **libopus-dev** (optional, for native opus — `sudo apt install libopus-dev`)

yt-dlp is automatically downloaded and updated by the bot on startup.

## Quick Start

```bash
# 1. Clone and enter the directory
git clone <repo-url>
cd NoraeGaori

# 2. Copy example configs
cp .env.example .env
cp config/config.example.json config/config.json
cp config/admins.example.json config/admins.json
cp config/rpcConfig.example.json config/rpcConfig.json

# 3. Edit .env with your bot token
#    DISCORD_BOT_TOKEN=your_token_here

# 4. Build and run
make setup
make run
```

Or step by step:

```bash
go mod download
make build
./noraegaori
```

## Configuration

### `.env`

| Variable | Required | Description |
|---|---|---|
| `DISCORD_BOT_TOKEN` | Yes | Your Discord bot token |
| `DEBUG_MODE` | No | Set to `true` for verbose logging |

### `config/config.json`

| Field | Default | Description |
|---|---|---|
| `prefix` | `!` | Command prefix for text commands |
| `language` | `en` | Bot language (`en`, `ko`) |
| `show_started_track` | `true` | Show "Now Playing" messages |
| `default_volume` | `100` | Default volume (0-1000) |
| `precache_strategy` | `1` | 0=None, 1=Full memory |
| `max_precache_memory` | `1` | Max pre-cache memory in GB |
| `max_download_speed_mbps` | `10` | Max download speed per server |

### `config/admins.json`

```json
{
  "admins": ["your_discord_user_id"]
}
```

### Language

Set `"language"` in `config/config.json` to change the bot's display language:
- `"en"` — English (default)
- `"ko"` — Korean

Locale files are in `locales/`. Missing translations automatically fall back to English.

To add a new language, create `locales/<code>.json` using `locales/en.json` as a template. Partial translations are fine — any missing string falls back to English.

## Commands

### Music Playback

| Command | Aliases | Description |
|---|---|---|
| `/play <query>` | `p` | Play a song from URL or search |
| `/playnext <query>` | `pn` | Add a song to play next |
| `/search <query>` | `s` | Search YouTube and pick a result |
| `/pause` | | Pause and leave channel (state preserved) |
| `/resume` | | Resume playback |
| `/skip` | | Vote to skip current song |
| `/stop` | `st` | Stop playback and clear queue |
| `/nowplaying` | `np` | Show the current song |
| `/volume [level]` | `vol`, `v` | Get or set volume (0-1000) |
| `/repeat [mode]` | | Set repeat: `on`, `single`, `off` |

### Queue Management

| Command | Aliases | Description |
|---|---|---|
| `/queue [page]` | `q` | Show the queue |
| `/remove <pos>` | `rm` | Remove a song (`3`, `1-5`, `ALL`) |
| `/swap <a> <b>` | | Swap two songs |
| `/skipto <pos>` | | Skip to a specific position |

### Voice

| Command | Aliases | Description |
|---|---|---|
| `/join [channel]` | `j` | Join a voice channel |
| `/leave` | `dc` | Leave the voice channel |
| `/switchvc [channel]` | `switch`, `move` | Move to another channel |

### Settings

| Command | Aliases | Description |
|---|---|---|
| `/sponsorblock [on/off]` | `sb` | Toggle SponsorBlock |
| `/showstartedtrack [on/off]` | `showtrack` | Toggle now-playing messages |
| `/normalization [on/off]` | `normalize` | Toggle volume normalization |

### Admin Only

| Command | Aliases | Description |
|---|---|---|
| `/setprefix <prefix>` | `prefix` | Change the command prefix |
| `/forceskip` | `fs` | Skip without voting |
| `/forceremove <target>` | `fr` | Remove a user's songs |
| `/forcestop` | `fstop` | Force stop and clear queue |
| `/movetrack <from> <to>` | `mt` | Move a song to a new position |
| `/status` | | Show system info |

## Project Structure

```
NoraeGaori/
├── cmd/bot/            Entry point
├── internal/
│   ├── bot/            Discord session and event handlers
│   ├── commands/       All command handlers
│   ├── config/         Config loading with hot-reload
│   ├── database/       SQLite
│   ├── messages/       Locale system and embed helpers
│   ├── player/         Audio streaming and voice
│   ├── queue/          Queue management with caching
│   ├── rpc/            Discord Rich Presence
│   └── youtube/        yt-dlp and InnerTube integration
├── locales/            Language files (ko.json, en.json)
├── config/             Runtime config (gitignored, see *.example.json)
├── pkg/logger/         Logging
├── Makefile
├── Dockerfile
└── docker-compose.yml
```

## Docker

```bash
make docker-build
make docker-run
```

Or with docker-compose:

```bash
docker-compose up -d
```

## Make Commands

| Command | Description |
|---|---|
| `make setup` | First-time setup (install deps + build) |
| `make build` | Build with native libopus (requires libopus-dev) |
| `make build-nonative` | Build with WASM opus (no system deps) |
| `make run` | Build and run |
| `make dev` | Run in dev mode with debug logging |
| `make clean` | Remove build artifacts |
| `make help` | Show all available commands |

## Disclaimer

This project is for **educational purposes only**. It is intended as a learning exercise in Go, Discord bot development, and audio streaming. The authors do not encourage or condone the use of this software to violate YouTube's Terms of Service or any applicable copyright laws. Use at your own risk.

## License

[PolyForm Noncommercial 1.0.0](LICENSE)