package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"
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

// NotificationMetadata stores the last notification message ID and removal state
type NotificationMetadata struct {
	NotificationMsgID int      // Message ID of the last DM notification
	RemovedAt         int64    // Unix timestamp when reaction was removed (0 if not removed)
	RemovedEmojis     []string // Emojis that were removed
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

func storeNotificationMetadata(userID, chatID int64, messageID int, meta *NotificationMetadata) {
	key := fmt.Sprintf("notification:%d:%d:%d", userID, chatID, messageID)
	data, err := json.Marshal(meta)
	if err != nil {
		log.Printf("failed to marshal notification metadata: %v", err)
		return
	}
	if err := store.Put(key, data); err != nil {
		log.Printf("failed to store notification metadata: %v", err)
	}
}

func getNotificationMetadata(userID, chatID int64, messageID int) *NotificationMetadata {
	key := fmt.Sprintf("notification:%d:%d:%d", userID, chatID, messageID)
	data, ok := store.Get(key)
	if !ok {
		return nil
	}
	var meta NotificationMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		log.Printf("failed to unmarshal notification metadata: %v", err)
		return nil
	}
	return &meta
}

func handleMessageReaction(ctx context.Context, b *bot.Bot, reaction *models.MessageReactionUpdated) {
	// Only handle reactions in group chats
	if reaction.Chat.Type == "private" {
		return
	}

	// Look up original message metadata
	metadata := getMessageMetadata(reaction.Chat.ID, reaction.MessageID)
	if metadata == nil {
		// Message not in cache (probably sent before bot restart)
		return
	}

	// Extract reactor information
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
		reactorName = "Anonymous"
	}

	// Build chat title
	chatTitle := reaction.Chat.Title
	if chatTitle == "" {
		chatTitle = "a chat"
	}

	// Build message link
	chatIDStr := fmt.Sprintf("%d", reaction.Chat.ID)
	if len(chatIDStr) > 4 && chatIDStr[:4] == "-100" {
		chatIDStr = chatIDStr[4:]
	}
	messageLink := fmt.Sprintf("https://t.me/c/%s/%d", chatIDStr, reaction.MessageID)

	// Look up previous notification metadata
	notificationMeta := getNotificationMetadata(metadata.UserID, reaction.Chat.ID, reaction.MessageID)

	// If user removed all reactions
	if len(reaction.NewReaction) == 0 {
		if notificationMeta != nil && notificationMeta.NotificationMsgID != 0 {
			// Extract old emojis from OldReaction
			var oldEmojis []string
			for _, r := range reaction.OldReaction {
				if r.ReactionTypeEmoji != nil {
					oldEmojis = append(oldEmojis, r.ReactionTypeEmoji.Emoji)
				} else if r.ReactionTypeCustomEmoji != nil {
					oldEmojis = append(oldEmojis, "[custom]")
				}
			}

			// Update notification to show "reacted and removed"
			emojiStr := strings.Join(oldEmojis, " ")
			notificationText := fmt.Sprintf(
				"🎭 <b>%s</b> reacted: %s and removed\n\n"+
					"<b>In:</b> %s\n\n"+
					"<a href=\"%s\">Jump to message</a>",
				reactorName,
				emojiStr,
				chatTitle,
				messageLink,
			)

			_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    metadata.UserID,
				MessageID: notificationMeta.NotificationMsgID,
				Text:      notificationText,
				ParseMode: models.ParseModeHTML,
			})
			if err != nil {
				log.Printf("edit notification for removal: %v", err)
			}

			// Store removal timestamp and emojis
			notificationMeta.RemovedAt = time.Now().Unix()
			notificationMeta.RemovedEmojis = oldEmojis
			storeNotificationMetadata(metadata.UserID, reaction.Chat.ID, reaction.MessageID, notificationMeta)

			// Start goroutine to delete after 30 seconds if no new reaction
			go func() {
				time.Sleep(30 * time.Second)

				// Re-fetch metadata to check if it's still in "removed" state
				currentMeta := getNotificationMetadata(metadata.UserID, reaction.Chat.ID, reaction.MessageID)
				if currentMeta != nil && currentMeta.RemovedAt == notificationMeta.RemovedAt && currentMeta.RemovedAt != 0 {
					// Still in removed state, delete the notification
					_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
						ChatID:    metadata.UserID,
						MessageID: currentMeta.NotificationMsgID,
					})
					if err != nil {
						log.Printf("delete notification after 30s: %v", err)
					}
					// Clear metadata
					storeNotificationMetadata(metadata.UserID, reaction.Chat.ID, reaction.MessageID, &NotificationMetadata{})
				}
			}()
		}
		return
	}

	// Extract emojis from new reaction
	var emojis []string
	for _, r := range reaction.NewReaction {
		if r.ReactionTypeEmoji != nil {
			emojis = append(emojis, r.ReactionTypeEmoji.Emoji)
		} else if r.ReactionTypeCustomEmoji != nil {
			emojis = append(emojis, "[custom]")
		}
	}

	// Build notification text
	emojiStr := strings.Join(emojis, " ")
	notificationText := fmt.Sprintf(
		"🎭 <b>%s</b> reacted: %s\n\n"+
			"<b>In:</b> %s\n\n"+
			"<a href=\"%s\">Jump to message</a>",
		reactorName,
		emojiStr,
		chatTitle,
		messageLink,
	)

	var newMessageID int

	// Try to edit previous notification if it exists
	if notificationMeta != nil && notificationMeta.NotificationMsgID != 0 {
		_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    metadata.UserID,
			MessageID: notificationMeta.NotificationMsgID,
			Text:      notificationText,
			ParseMode: models.ParseModeHTML,
		})
		if err != nil {
			log.Printf("edit notification failed (not latest message or deleted): %v", err)
			// Edit failed, will send new message below
		} else {
			// Edit succeeded, keep the same message ID
			newMessageID = notificationMeta.NotificationMsgID
		}
	}

	// If edit failed or no previous notification, send new message
	if newMessageID == 0 {
		sentMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:              metadata.UserID,
			Text:                notificationText,
			ParseMode:           models.ParseModeHTML,
			DisableNotification: true, // Silent notification
		})
		if err != nil {
			log.Printf("send reaction notification to user %d: %v", metadata.UserID, err)
			return
		}
		newMessageID = sentMsg.ID
	}

	// Store notification message ID and clear removal state
	newNotificationMeta := &NotificationMetadata{
		NotificationMsgID: newMessageID,
		RemovedAt:         0,
		RemovedEmojis:     nil,
	}
	storeNotificationMetadata(metadata.UserID, reaction.Chat.ID, reaction.MessageID, newNotificationMeta)
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
