# bsky-spoiler-telegram-bot

A Telegram bot that gets images from bluesky post and sends them spoilered in Telegram chats.

## Description

This bot listens for the `/spoiler` command followed by a Bluesky post URL. It fetches the images from the specified Bluesky post and sends them as spoiler media in the Telegram chat, then deletes the original command message to keep the chat clean.

## Features

- Works with "private" bluesky profiles
- Doesn't require bsky auth, uses public api
- Supports multiple images in the post
- Automatic deletion of the command message (requires permission to delete messages in chat)
- Janky error handling with user-friendly messages

## Requirements

- Go 1.25.7 or later, may work on older version, but I haven't tested it tbh
- A Telegram bot token (obtain from [@BotFather](https://t.me/botfather))

## Installation

1. Clone the repository:

   ```bash
   git clone https://github.com/yourusername/bsky-inline-spoiler.git
   cd bsky-inline-spoiler
   ```

2. Install dependencies:

   ```bash
   go mod download
   ```

3. Build the bot:

   ```bash
   go build -o bsky-inline-spoiler
   ```

## Setup

1. Create a `.env` file in the project root with your Telegram bot token:

   ```env
   TELEGRAM_BOT_TOKEN=your_bot_token_here
   ```

2. Run the bot:

   ```bash
   ./bsky-inline-spoiler
   ```

## Usage

1. Add the bot to your Telegram chat or group.
2. Ensure the bot has permission to delete messages (for automatic cleanup).
3. Use the command: `/spoiler <Bluesky post URL>`

Example:

```
/spoiler https://bsky.app/profile/username.bsky.social/post/abc123def456
```

The bot will:

- React with a 👌 emoji to acknowledge the command
- Fetch images from the Bluesky post
- Send them as spoiler media in the chat
- Delete the original command message (requires permission to delete messages in chat)

## Dependencies

- [github.com/go-telegram/bot](https://github.com/go-telegram/bot) - Telegram Bot API wrapper
- [github.com/bluesky-social/indigo](https://github.com/bluesky-social/indigo) - Bluesky/AT Protocol client
- [github.com/joho/godotenv](https://github.com/joho/godotenv) - Environment variable loader

## License

MIT
