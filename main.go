package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"unicode/utf8"

	"github.com/TwinProduction/gdstore"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	_ "github.com/joho/godotenv/autoload"
)

// MessageMetadata stores info about sent messages for reaction handling
type MessageMetadata struct {
	UserID    int64
	ChatID    int64
	MessageID int
}

var store *gdstore.GDStore

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	// Initialize gdstore for message metadata persistence
	store = gdstore.New("message_metadata.db")
	defer store.Close()

	bskyClient := NewBlueskyClient()

	b, err := bot.New(token, bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage, models.AllowedUpdateMessageReaction}))
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleStartCommand(ctx, b, update.Message)
	})

	b.RegisterHandler(bot.HandlerTypeMessageText, "/spoiler", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleSpoilerCommand(ctx, b, update.Message, bskyClient)
	})

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.MessageReaction != nil
	}, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleMessageReaction(ctx, b, update.MessageReaction)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Println("Bot started")
	b.Start(ctx)
}

func handleStartCommand(ctx context.Context, b *bot.Bot, msg *models.Message) {
	welcomeText := `👋 Welcome to Bluesky Spoiler Bot!

This bot fetches images from Bluesky posts and sends them as spoilered media in Telegram.

<b>Usage:</b>
<code>/spoiler &lt;Bluesky post URL&gt;</code>

<b>Example:</b>
<code>/spoiler https://bsky.app/profile/username.bsky.social/post/abc123</code>

<b>Supported domains:</b>
— bsky.app, fxbsky.app, vxbsky.app
— bskye.app, bskyx.app, bsyy.app

<b>Features:</b>
— Works with "private" Bluesky profiles
— Supports multiple images per post
— Automatically deletes command messages (requires delete permission)
— Sends reaction notifications to your DM when someone reacts to your spoilered posts

<b>Note:</b> You can just send this bot the command without requiring it to be added to the group you don't own`

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      welcomeText,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		log.Printf("sending start message: %v", err)
	}
}

func handleSpoilerCommand(ctx context.Context, b *bot.Bot, msg *models.Message, bskyClient *BlueskyClient) {
	text := msg.Text
	arg := ""
	if len(text) > 8 {
		arg = text[8:]
	}
	if len(arg) > 0 && arg[0] == '@' {
		if idx := indexOf(arg, ' '); idx >= 0 {
			arg = arg[idx:]
		} else {
			arg = ""
		}
	}
	arg = trimSpace(arg)

	if utf8.RuneCountInString(arg) == 0 {
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    msg.Chat.ID,
			Text:      "Usage: ```command\n" + bot.EscapeMarkdown("/spoiler <bsky.app post URL>") + "```",
			ParseMode: models.ParseModeMarkdown,
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		if err != nil {
			log.Printf("sending message with help: %v", err)
		}
		return
	}

	parsed, err := ParseBlueskyURL(arg)
	if err != nil {
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Please provide a valid Bluesky post URL (supports bsky.app, fxbsky.app, vxbsky.app, bskye.app, bskyx.app, bsyy.app).",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		if err != nil {
			log.Printf("sending message with help 2: %v", err)
		}
		return
	}

	_, err = b.SetMessageReaction(ctx, &bot.SetMessageReactionParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		Reaction: []models.ReactionType{
			models.ReactionType{
				Type: models.ReactionTypeTypeEmoji,
				ReactionTypeEmoji: &models.ReactionTypeEmoji{
					Emoji: "👌",
					Type:  models.ReactionTypeTypeEmoji,
				},
			},
		},
	})

	if err != nil {
		log.Printf("SetMessageReaction: %v", err)
		return
	}

	did, err := bskyClient.ResolveToDID(ctx, parsed.Authority)
	if err != nil {
		log.Printf("resolve DID: %v", err.Error())
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Failed to resolve Bluesky profile.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		return
	}

	atURI := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, parsed.Rkey)
	images, err := bskyClient.FetchPostImages(ctx, atURI)
	if err != nil {
		log.Printf("fetch images: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Failed to fetch post images.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		return
	}

	if len(images) == 0 {
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "No images found in that post.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		if err != nil {
			log.Printf("SendMessage: No images found in that post: %v", err)
		}
		return
	}

	if len(images) == 1 {
		sentMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID: msg.Chat.ID,
			Photo:  &models.InputFileString{Data: images[0].Fullsize},
			Caption: fmt.Sprintf(
				`<a href="%s">%s</a> (%s) sent:`+"\n%s",
				"t.me/"+msg.From.Username,
				msg.From.FirstName,
				"@"+msg.From.Username,
				parsed.OriginalURL,
			),
			ParseMode:             models.ParseModeHTML,
			HasSpoiler:            true,
			ShowCaptionAboveMedia: true,
		})
		if err != nil {
			log.Printf("SendPhoto: %v", err)
			return
		}

		// Store message metadata for reaction handling
		storeMessageMetadata(msg.Chat.ID, sentMsg.ID, msg.From.ID)

		_, err = b.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
		})
		if err != nil {
			log.Println("Can't delete sender's message, does bot have permission to delete messages?", err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text: "Can't delete sender's message, does bot have permission to delete messages?" +
					"```error\n" +
					err.Error() +
					"```",
				ParseMode: models.ParseModeMarkdown,
			})
		}
		return
	}

	media := make([]models.InputMedia, len(images))
	for i, img := range images {
		p := &models.InputMediaPhoto{
			Media:                 img.Fullsize,
			HasSpoiler:            true,
			ShowCaptionAboveMedia: true,
		}
		if i == 0 {
			p.Caption = fmt.Sprintf(
				`<a href="%s">%s</a> (%s) sent:`+"\n%s",
				"t.me/"+msg.From.Username,
				msg.From.FirstName,
				"@"+msg.From.Username,
				parsed.OriginalURL,
			)
			p.ParseMode = models.ParseModeHTML
		}
		media[i] = p
	}
	sentMsgs, err := b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
		ChatID: msg.Chat.ID,
		Media:  media,
	})

	if err != nil {
		log.Println("Can't send media group: ", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text: "Can't send media group: " +
				"```error\n" +
				err.Error() +
				"```",
			ParseMode: models.ParseModeMarkdown,
		})
		return
	}

	// Store message metadata for all messages in the group
	for _, sentMsg := range sentMsgs {
		storeMessageMetadata(msg.Chat.ID, sentMsg.ID, msg.From.ID)
	}

	_, err = b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
	})
	if err != nil {
		log.Println("Can't delete sender's message, does bot have permission to delete messages?", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text: "Can't delete sender's message, does bot have permission to delete messages?" +
				"```error\n" +
				err.Error() +
				"```",
			ParseMode: models.ParseModeMarkdown,
		})
	}
}

func storeMessageMetadata(chatID int64, messageID int, userID int64) {
	key := fmt.Sprintf("%d:%d", chatID, messageID)
	metadata := &MessageMetadata{
		UserID:    userID,
		ChatID:    chatID,
		MessageID: messageID,
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		log.Printf("failed to marshal metadata: %v", err)
		return
	}
	if err := store.Put(key, data); err != nil {
		log.Printf("failed to store metadata: %v", err)
	}
}

func getMessageMetadata(chatID int64, messageID int) *MessageMetadata {
	key := fmt.Sprintf("%d:%d", chatID, messageID)
	data, ok := store.Get(key)
	if !ok {
		return nil
	}
	var metadata MessageMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		log.Printf("failed to unmarshal metadata: %v", err)
		return nil
	}
	return &metadata
}

func handleMessageReaction(ctx context.Context, b *bot.Bot, reaction *models.MessageReactionUpdated) {
	// Only handle reactions in group chats
	if reaction.Chat.Type == "private" {
		return
	}

	// Get new reactions (added reactions)
	if len(reaction.NewReaction) == 0 {
		return
	}

	// Look up original message metadata
	metadata := getMessageMetadata(reaction.Chat.ID, reaction.MessageID)
	if metadata == nil {
		// Message not in cache (probably sent before bot restart)
		return
	}

	// Build reaction message
	var reactorName string
	if reaction.User != nil {
		if reaction.User.Username != "" {
			reactorName = fmt.Sprintf("@%s", reaction.User.Username)
		} else {
			reactorName = reaction.User.FirstName
		}
	} else if reaction.ActorChat != nil {
		reactorName = reaction.ActorChat.Title
	} else {
		reactorName = "Someone"
	}

	var reactionEmojis string
	for i, r := range reaction.NewReaction {
		if i > 0 {
			reactionEmojis += " "
		}
		if r.ReactionTypeEmoji != nil {
			reactionEmojis += r.ReactionTypeEmoji.Emoji
		} else if r.ReactionTypeCustomEmoji != nil {
			reactionEmojis += "[custom]"
		}
	}

	chatTitle := reaction.Chat.Title
	if chatTitle == "" {
		chatTitle = "a chat"
	}

	// Build message link
	// For supergroups/channels, chat ID needs to be converted (remove -100 prefix)
	chatIDStr := fmt.Sprintf("%d", reaction.Chat.ID)
	if len(chatIDStr) > 4 && chatIDStr[:4] == "-100" {
		chatIDStr = chatIDStr[4:]
	}
	messageLink := fmt.Sprintf("https://t.me/c/%s/%d", chatIDStr, reaction.MessageID)

	notificationText := fmt.Sprintf(
		"🎭 <b>New reaction to your spoilered post!</b>\n\n"+
			"<b>From:</b> %s\n"+
			"<b>In:</b> %s\n"+
			"<b>Reaction:</b> %s\n\n"+
			"<a href=\"%s\">Jump to message</a>",
		reactorName,
		chatTitle,
		reactionEmojis,
		messageLink,
	)

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    metadata.UserID,
		Text:      notificationText,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		log.Printf("send reaction notification to user %d: %v", metadata.UserID, err)
	}
}

func indexOf(s string, b byte) int {
	for i := range s {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
