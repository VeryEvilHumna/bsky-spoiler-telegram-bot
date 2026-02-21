# bsky-spoiler-telegram-bot

A Telegram bot that gets images/video from Bluesky posts and sends them in Telegram chats, optionally blurred as spoilers.

Hosted version available: [t.me/bskyWithSpoilerBot](https://t.me/bskyWithSpoilerBot). Just add it in your group and give permission to delete messages

## Description

This bot listens for `/spoiler` or `/nospoiler` commands followed by a Bluesky post URL. It fetches the images or video from the specified post and sends them in the Telegram chat, then deletes the original command message to keep the chat clean. `/spoiler` blurs the media until tapped; `/nospoiler` sends it unblurred. Either way, the post text is tucked away under a collapsible blockquote.

## Features

- `/spoiler` — sends media blurred until tapped
- `/nospoiler` — sends media unblurred; post text still collapsed
- Works with "private" Bluesky profiles
- Doesn't require Bluesky auth, uses the public API
- Supports images and video posts
- Supports multiple images in the post
- Supports multiple embed domains (bsky.app, fxbsky.app, vxbsky.app, bskye.app, bskyx.app, bsyy.app)
- Automatic deletion of the command message (requires permission to delete messages in chat)
- Optional content warning text appended after the post link in the caption
- Post text revealed as a collapsible blockquote — tap to expand and see what the post said
- Smart reaction notifications: get silent DM notifications when someone reacts to your post
  - Updates existing notification if it's still relevant
  - Shows "reacted and removed" when users unreact, auto-deletes after 30s if no new reaction
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
3. Use one of the commands:

```
/spoiler <Bluesky post URL> [content warning]
/nospoiler <Bluesky post URL> [content warning]
```

Examples:

```
/spoiler https://bsky.app/profile/username.bsky.social/post/abc123def456
/spoiler https://bsky.app/profile/username.bsky.social/post/abc123def456 body horror
/nospoiler https://bsky.app/profile/username.bsky.social/post/abc123def456
/nospoiler https://bsky.app/profile/username.bsky.social/post/abc123def456 cute kitty video
```

The bot will:

- React with a 👌 emoji to acknowledge the command
- Fetch the images or video from the Bluesky post
- Send them in the chat (blurred with `/spoiler`, unblurred with `/nospoiler`), with the post text attached as a collapsible blockquote
- Delete the original command message (requires permission to delete messages in chat)
- Send you silent DM notifications when someone reacts to your post
  - Notifications update intelligently (edits if still last message, sends new otherwise)
  - When users unreact: shows "reacted and removed", auto-deletes after 30s if no new reaction

## Dependencies

- [github.com/go-telegram/bot](https://github.com/go-telegram/bot) - Telegram Bot API wrapper
- [github.com/bluesky-social/indigo](https://github.com/bluesky-social/indigo) - Bluesky/AT Protocol client
- [github.com/joho/godotenv](https://github.com/joho/godotenv) - Environment variable loader

## License

MIT
