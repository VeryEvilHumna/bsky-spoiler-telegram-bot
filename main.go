package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/TwinProduction/gdstore"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	_ "github.com/joho/godotenv/autoload"
)

var store *gdstore.GDStore
var pendingRequests sync.Map
var pendingEmbeds sync.Map

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	store = gdstore.New("message_metadata.db")
	defer store.Close()

	bskyClient := NewBlueskyClient()

	inkbunnyUsername := os.Getenv("INKBUNNY_USERNAME")
	inkbunnyPassword := os.Getenv("INKBUNNY_PASSWORD")
	if inkbunnyUsername != "" {
		log.Printf("Inkbunny: authenticated as %s", inkbunnyUsername)
	} else {
		log.Println("Inkbunny: guest mode (set INKBUNNY_USERNAME/INKBUNNY_PASSWORD for full access)")
	}
	inkbunnyClient := NewInkbunnyClient(inkbunnyUsername, inkbunnyPassword)

	igCookies = loadIGCookieJar(os.Getenv("INSTAGRAM_COOKIES_FILE"))
	defer igCookies.Save()

	b, err := bot.New(token, bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage, models.AllowedUpdateMessageReaction, models.AllowedUpdateCallbackQuery}))
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleStartCommand(ctx, b, update.Message)
	})

	b.RegisterHandler(bot.HandlerTypeMessageText, "/spoiler", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleMediaCommand(ctx, b, update.Message, bskyClient, inkbunnyClient, true)
	})

	b.RegisterHandler(bot.HandlerTypeMessageText, "/nospoiler", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleMediaCommand(ctx, b, update.Message, bskyClient, inkbunnyClient, false)
	})

	b.RegisterHandler(bot.HandlerTypeMessageText, "/delete", bot.MatchTypeExact, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleDeleteCommand(ctx, b, update.Message)
	})

	b.RegisterHandler(bot.HandlerTypeMessageText, "/d", bot.MatchTypeExact, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleDeleteCommand(ctx, b, update.Message)
	})

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.MessageReaction != nil
	}, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleMessageReaction(ctx, b, update.MessageReaction)
	})

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "embed:", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleEmbedCallback(ctx, b, update)
	})

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.Message != nil &&
			update.Message.Text != "" &&
			!strings.HasPrefix(update.Message.Text, "/") &&
			update.Message.From != nil
	}, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handlePlainMessage(ctx, b, update.Message, bskyClient, inkbunnyClient)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Println("Bot started")
	b.Start(ctx)
	log.Println("Bot stopped")
}
