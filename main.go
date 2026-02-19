package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	bskyClient := NewBlueskyClient()

	b, err := bot.New(token)
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/spoiler", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleSpoilerCommand(ctx, b, update.Message, bskyClient)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Println("Bot started")
	b.Start(ctx)
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
			Text:   "Please provide a valid bsky.app post URL.",
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
		log.Printf("resolve DID: %v", err)
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
		_, err = b.SendPhoto(ctx, &bot.SendPhotoParams{
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
	_, err = b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
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
