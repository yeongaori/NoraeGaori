# NoraeGaori

A feature-rich & high-quality audio Discord music bot written in Go.

## Features

- **High-Quality Audio Streaming**
- **Persistent Queue**
- **AutoMix**
- **Crossfade**
- **Fade-In / Fade-Out**
- **Trim Silence**
- **SponsorBlock**
- **Live Stream Support**
- **Queue Management**
- **Skip/Stop Votes**
- **Per-Guild Settings**
- **Auto Pause**
- **Slash Commands & Prefix Commands**
- **Multi-Language Support**
- **Admin Commands**
- **Hot-Reload Config**
- **yt-dlp auto-updater**

## [Latest release](https://github.com/yeongaori/NoraeGaori/releases/latest)

| Service                                                           | Status                                                                                                                                                                      |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [GitHub actions](https://github.com/yeongaori/NoraeGaori/actions) | [![.github/workflows/ci.yml](https://github.com/yeongaori/NoraeGaori/actions/workflows/ci.yml/badge.svg)](https://github.com/yeongaori/NoraeGaori/actions/workflows/ci.yml) |

## Prerequisites

- **Go** 1.25+
- **FFmpeg** (`sudo apt install ffmpeg`)
- **libopus** (optional, for native encoding — `sudo apt install libopus0`)
  - The bot loads libopus at runtime via dlopen. If the library is found, the
    native encoder is used; if not, a pure-Go WASM encoder is used as a fallback
    (a warning is logged at startup). Released binaries do not need libopus
    installed to run.

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

| Variable            | Required | Description                                       |
| ------------------- | -------- | ------------------------------------------------- |
| `DISCORD_BOT_TOKEN` | Yes      | Your Discord bot token                            |
| `DEBUG_MODE`        | No       | Set to `true` for verbose bot logging             |
| `DISCORDGO_DEBUG`   | No       | Set to `true` for discordgo library debug logging |

### `config/config.json`

| Field                     | Default      | Description                                                                                                                                                                                    |
| ------------------------- | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `prefix`                  | `!`          | Default command prefix for text commands; each server can override with `setprefix`                                                                                                            |
| `language`                | `en`         | Default bot language (`en`, `ko`); each server can override with `setlanguage`                                                                                                                 |
| `show_started_track`      | `true`       | Show "Now Playing" messages                                                                                                                                                                    |
| `default_volume`          | `100`        | Default volume (0-1000)                                                                                                                                                                        |
| `max_download_speed_mbps` | `10`         | Max download speed per server                                                                                                                                                                  |
| `log_file`                | `latest.log` | Save all terminal output to this file; `off` disables                                                                                                                                          |
| `ytdlp_channel`           | `auto`       | yt-dlp release channel. `auto` runs stable and switches to nightly only while stable fails its canary, returning to stable once a working stable ships. `stable` or `nightly` pins one channel |

### `config/admins.json`

```json
{
    "admins": ["your_discord_user_id"]
}
```

### Language

`"language"` in `config/config.json` sets the bot-wide default:

- `"en"` — English (default)
- `"ko"` — Korean

Each server can override this with the `setlanguage <code>` text command (admin
only). Run `setlanguage` with no argument to see the current language; pass an
empty value to clear the override and fall back to the default.

Locale files are in `locales/`. Missing translations automatically fall back to
English.

To add a new language, create `locales/<code>.json` using `locales/en.json` as a
template. Partial translations are fine — any missing string falls back to
English.

## Commands

### Music Playback

| Command             | Aliases    | Description                                                             |
| ------------------- | ---------- | ----------------------------------------------------------------------- |
| `/play <query>`     | `p`        | Play a song from URL or search                                          |
| `/playnext <query>` | `pn`       | Add a song to play next                                                 |
| `/search <query>`   | `s`        | Search YouTube and pick a result                                        |
| `/pause`            |            | Pause and leave channel (state preserved)                               |
| `/resume`           |            | Resume playback                                                         |
| `/skip`             |            | Vote to skip current song                                               |
| `/seek <position>`  | `jump`     | Jump to a position in the current song (e.g. `1:23`, `83`, or `1:21.5`) |
| `/stop`             | `st`       | Stop playback and clear queue                                           |
| `/nowplaying`       | `np`       | Show the current song                                                   |
| `/volume [level]`   | `vol`, `v` | Get or set volume (0-1000)                                              |
| `/repeat [mode]`    |            | Set repeat: `on`, `single`, `off`                                       |

### Queue Management

| Command         | Aliases | Description                       |
| --------------- | ------- | --------------------------------- |
| `/queue [page]` | `q`     | Show the queue                    |
| `/remove <pos>` | `rm`    | Remove a song (`3`, `1-5`, `ALL`) |
| `/swap <a> <b>` |         | Swap two songs                    |
| `/skipto <pos>` |         | Skip to a specific position       |

### Voice

| Command               | Aliases          | Description             |
| --------------------- | ---------------- | ----------------------- |
| `/join [channel]`     | `j`              | Join a voice channel    |
| `/leave`              | `dc`             | Leave the voice channel |
| `/switchvc [channel]` | `switch`, `move` | Move to another channel |

### Settings

| Command                            | Aliases     | Description                                                                                                                                                                     |
| ---------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/sponsorblock [on/off]`           | `sb`        | Toggle SponsorBlock                                                                                                                                                             |
| `/showstartedtrack [on/off]`       | `showtrack` | Toggle now-playing messages                                                                                                                                                     |
| `/normalization [on/off]`          | `normalize` | Toggle volume normalization                                                                                                                                                     |
| `/fadein [on/off] [seconds]`       | `fade-in`   | Fade in at song start, on seek, and on resume (1-30s, default 3)                                                                                                                |
| `/fadeout [on/off] [seconds]`      | `fade-out`  | Fade out at song end and before seek (1-30s, default 3)                                                                                                                         |
| `/automix [on/off] [beats]`        | `mix`       | Beat-aware crossfade between songs (4-64 beats, default 16)                                                                                                                     |
| `/automixstyle [category] [style]` | `mixstyle`  | Set the server-wide AutoMix style for a category (`volume`, `eq`, `filter`, `effect`, `loop`); `auto` lets AutoMix choose per transition. No arguments shows the current styles |
| `/automixpanel [page]`             | `mixpanel`  | Open the AutoMix panel to override the transition style for a single upcoming song                                                                                              |
| `/crossfade [on/off] [seconds]`    | `cf`        | Crossfade between songs (1-30s, default 8)                                                                                                                                      |
| `/fadeonstop [on/off]`             | `fos`       | Fade out briefly before skip/stop instead of cutting                                                                                                                            |
| `/trimsilence [on/off]`            | `trim`      | Skip silence at the start and end of songs (always active while AutoMix is on)                                                                                                  |

### Admin Only

Admin commands are **text-only** (prefix commands, not slash commands). Invoke
them with the configured prefix — the server's override if set, otherwise the
global default — e.g. `!setprefix #`.

| Command                 | Aliases                       | Description                                                                                       |
| ----------------------- | ----------------------------- | ------------------------------------------------------------------------------------------------- |
| `setprefix [prefix]`    | `prefix`                      | Change this server's command prefix (no argument shows current; empty argument resets to default) |
| `setlanguage [code]`    | `setlang`, `language`, `lang` | Set server language (`en`, `ko`); no argument shows the current language                          |
| `forceskip`             | `fs`                          | Skip without voting                                                                               |
| `forceremove <target>`  | `fr`                          | Remove a user's songs                                                                             |
| `forcestop`             | `fstop`                       | Force stop and clear queue                                                                        |
| `movetrack <from> <to>` | `mt`                          | Move a song to a new position                                                                     |
| `status`                |                               | Show system info                                                                                  |

### Help

| Command        | Aliases | Description                |
| -------------- | ------- | -------------------------- |
| `/help [page]` | `h`     | Show help for all commands |

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

| Command      | Description                                                        |
| ------------ | ------------------------------------------------------------------ |
| `make setup` | First-time setup (install deps + build)                            |
| `make build` | Build the bot (uses libopus at runtime if present, WASM otherwise) |
| `make run`   | Build and run                                                      |
| `make dev`   | Run in dev mode with debug logging                                 |
| `make clean` | Remove build artifacts                                             |
| `make help`  | Show all available commands                                        |

## Disclaimer

This project is for **educational purposes only**. It is intended as a learning
exercise in Go, Discord bot development, and audio streaming. The authors do not
encourage or condone the use of this software to violate YouTube's Terms of
Service or any applicable copyright laws. Use at your own risk.

## License

[PolyForm Noncommercial 1.0.0](LICENSE)
