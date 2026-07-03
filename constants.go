package main

import "time"

const (
	maxImagesPerPost    = 10
	autoDeleteDelay     = 10 * time.Second
	promptDeleteDelay   = 30 * time.Second
	httpTimeout         = 120 * time.Second
	inkbunnyReferrer    = "https://inkbunny.net/"
	telegramGroupPrefix = "-100"
	telegramGroupIDBase = 1_000_000_000
)

const welcomeText = `👋 Welcome to Bluesky Spoiler Bot!

This bot fetches images from Bluesky, Twitter/X, and Inkbunny posts and sends them as spoilered media in Telegram.

<b>Usage:</b>
<code>/spoiler &lt;post URL&gt; [content warning]</code>
<code>/nospoiler &lt;post URL&gt; [content warning]</code>

<b>Examples:</b>
<code>/spoiler https://bsky.app/profile/username.bsky.social/post/abc123</code>
<code>/spoiler https://x.com/username/status/123456789 body horror</code>
<code>/spoiler https://inkbunny.net/s/3900461</code>
<code>/nospoiler https://inkbunny.net/s/3900461 nudity</code>

<b>Supported domains:</b>
— bsky.app, fxbsky.app, vxbsky.app
— bskye.app, bskyx.app, bsyy.app
— x.com, twitter.com
— fxtwitter.com, vxtwitter.com, fixupx.com
— stupidpenisx.com, cunnyx.com, skibidix.com, girlcockx.com
— inkbunny.net
— at:// URIs (e.g. <code>at://did:plc:xxx/app.bsky.feed.post/...</code>)

<b>Features:</b>
— Works with "private" Bluesky profiles
— Supports multiple images per post
— Optional content warning text appended after the link
— Post text shown as a collapsible spoiler blockquote
— <code>/nospoiler</code> sends media without blur, text still collapsed
— Automatically deletes command messages (requires delete permission)
— Sends reaction notifications to your DM when someone reacts to your spoilered posts

<b>Note:</b> You can just send this bot the command without requiring it to be added to the group you don't own`
