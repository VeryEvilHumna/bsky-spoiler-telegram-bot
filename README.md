# bsky-spoiler-telegram-bot

Hosted version available: [t.me/bskyWithSpoilerBot](https://t.me/bskyWithSpoilerBot). Just add it in your group and give permission to delete messages

## Description

A Telegram bot that gets images/video from Bluesky, Twitter/X, Inkbunny, and Instagram posts and sends them in Telegram chats, optionally blurred as spoilers.

Just send a message with a supported link and the bot will ask if you want to embed it — no commands needed. You can also use `/spoiler` or `/nospoiler` for more control.

## Features

- **Auto-embed prompt** — just send a message with a supported link and the bot silently replies with Spoiler/No Spoiler buttons (no command needed)
- `/spoiler` — sends media blurred until tapped
- `/nospoiler` — sends media unblurred; post text still collapsed
- `/delete` (or `/d`) — delete your own bot-sent posts by replying to them (within 48 hours)
- Multi-platform support:
  - **Bluesky** — works with "private" profiles, no auth required (public API)
  - **Twitter/X** — via [fxtwitter](https://github.com/FixedDev/FxTwitter) API, no API key needed
  - **Inkbunny** — with BB code to HTML conversion for descriptions
  - **Instagram** — posts, reels, stories, carousels, share links; requires cookie auth
- Supports images and video posts
- Supports multiple images in the post
- Supported domains:
  - Bluesky: bsky.app, fxbsky.app, vxbsky.app, bskye.app, bskyx.app, bsyy.app
  - Twitter/X: x.com, twitter.com, fxtwitter.com, vxtwitter.com, fixupx.com, stupidpenisx.com, cunnyx.com, skibidix.com, girlcockx.com
  - Inkbunny: inkbunny.net
  - Instagram: instagram.com, ddinstagram.com
  - AT URIs: `at://did:plc:xxx/app.bsky.feed.post/...`
- Automatic cleanup:
  - Command messages are deleted after processing
  - Embed prompt auto-deletes after 30 seconds if ignored
  - Clicking the embed button on your own link-only message deletes the original too
  - Replying with `/spoiler` or `/nospoiler` to your own link-only message also deletes the original
- Optional content warning text appended after the post link in the caption
- Post text revealed as a collapsible blockquote — tap to expand and see what the post said
- Smart reaction notifications: get silent DM notifications when someone reacts to your post
  - Updates existing notification if it's still relevant
  - Shows "reacted and removed" when users unreact, auto-deletes after 30s if no new reaction
- Two-step link flow: send the command without a URL and the bot will ask for the link in the next message (requires the bot to have access to all messages in the chat)
- Janky error handling with user-friendly messages

## Requirements

- Go 1.25.7 or later, may work on older version, but I haven't tested it tbh
- A Telegram bot token (obtain from [@BotFather](https://t.me/botfather))
- Instagram cookies JSON file (for Instagram support — export via a browser extension like Cookie Editor)

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
   # Instagram cookies JSON file (required for Instagram support)
   # Export from instagram.com via a browser extension (e.g. Cookie Editor) as a JSON array
   INSTAGRAM_COOKIES_FILE=
   ```

2. Run the bot:

   ```bash
   ./bsky-inline-spoiler
   ```

## Usage

1. Add the bot to your Telegram chat or group.
2. Ensure the bot has permission to delete messages (for automatic cleanup).
3. Optionally, disable privacy mode (via [@BotFather](https://t.me/botfather)) or make the bot an admin to enable the two-step link flow and auto-embed detection.

### Auto-embed (recommended)

Just send a message containing a supported link. The bot will silently reply with two buttons:

- **Spoiler** — embeds the post with blurred media
- **No Spoiler** — embeds the post unblurred

The prompt auto-deletes after 30 seconds. If you click the button on your own link-only message, the bot also deletes the original message to keep the chat clean.

### Commands

```
/spoiler <post URL> [content warning]
/nospoiler <post URL> [content warning]
/delete
```

Examples:

```
/spoiler https://bsky.app/profile/username.bsky.social/post/abc123def456
/spoiler https://x.com/spicy_mochi/status/2072719566233284830 body horror
/nospoiler https://inkbunny.net/s/525390
/nospoiler https://inkbunny.net/s/525390 nudity
/spoiler https://www.instagram.com/p/abc123/
```

The bot will:

- React with a 👌 emoji to acknowledge the command
- Fetch the images or video from the post
- Send them in the chat (blurred with `/spoiler`, unblurred with `/nospoiler`), with the post text attached as a collapsible blockquote
- Delete the original command message (requires permission to delete messages in chat)
- If sent without a URL, ask for the link in a follow-up message — then process it and clean up all related messages automatically (requires access to all messages)
- If you reply with `/spoiler` or `/nospoiler` to your own message that starts with a link, the original message is also deleted

### Deleting posts

Reply to any bot-sent message with `/delete` or `/d` to remove it. You can only delete your own posts, and only within 48 hours of when they were sent. The delete command message is also cleaned up automatically.

### Reactions

- Get silent DM notifications when someone reacts to your embedded posts
- Notifications update intelligently (edits if still last message, sends new otherwise)
- When users unreact: shows "reacted and removed", auto-deletes after 30s if no new reaction

## Dependencies

- [github.com/go-telegram/bot](https://github.com/go-telegram/bot) - Telegram Bot API wrapper
- [github.com/bluesky-social/indigo](https://github.com/bluesky-social/indigo) - Bluesky/AT Protocol client
- [github.com/joho/godotenv](https://github.com/joho/godotenv) - Environment variable loader

## Credits

- [fxembed.com](https://fxembed.com/) — Twitter/X embed API used for fetching tweets without official API access

## License

MIT
