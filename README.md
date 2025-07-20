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
	- **-url (string): The URL to a song (e.g., from song.link). (required)
	- **-start (int): The starting point in the video, in seconds. (required)
	- **-duration (int): The duration of the resulting video clip. Must be between 10 and 59 seconds. (required)
	- **-name (string): A custom display text for the song. If provided, it will be used instead of the automatically parsed title and artist. (optional)
	- **-t (bool): A flag to send the video to the test channel (chat_id_test from your config). (optional)
### Examples

Create a 30-second clip starting at 45 seconds:

    go run main.go -url https://song.link/i/example -start 45 -duration 30

Create a 20-second clip starting at 1 minute and send it to the test channel:

    go run main.go -url https://song.link/i/example -start 60 -duration 20 -t

Create a 15-second clip with a custom name:

    go run main.go -url https://song.link/s/example -start 125 -duration 15 -name "Awesome Guitar Solo"
