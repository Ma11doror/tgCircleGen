# Telegram Circle Video Message Generator

## Prerequisites

Before running the application, ensure you have the following installed:

- **Go** (1.16 or later)
- **yt-dlp** - for downloading YouTube videos
- **ffmpeg** - for video processing

## How to:

### 1. Create a Telegram Bot

- Create a new bot and get your bot token
- Add the bot to your channel as an administrator

### 2. Create config.json

    {
      "bot_token": "YOUR_BOT_TOKEN_HERE",
      "chat_id": "@YourMainChannel",
      "chat_id_test": "@YourTestChannel"
    }

## Usage

### Basic Command Structure

    go run main.go <song.link_URL> <start_time_seconds> <duration_seconds> [flags]

### Parameters

- **song.link_URL**: The song.link URL (e.g., https://song.link/i/example)
- **start_time_seconds**: Starting point in the video (in seconds)
- **duration_seconds**: Duration of the resulting video clip (10-59 seconds)
- **-t** (optional): Test flag - sends to chat_id_test instead of main channel

### Examples

Create a 30-second clip starting at 45 seconds:

    go run main.go https://song.link/i/example 45 30

Create a 20-second clip starting at 1 minute, send to test channel:

    go run main.go https://song.link/i/example 60 20 -t
