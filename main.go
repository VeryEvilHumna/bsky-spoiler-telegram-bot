package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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

// PendingRequest tracks users who sent a command without a URL and are expected to reply with one
type PendingRequest struct {
	HasSpoiler bool
	BotMsgID   int // bot's "please send link" message
	CmdMsgID   int // original command message
}

var store *gdstore.GDStore
var pendingRequests sync.Map // key: "chatID:userID"

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	// Initialize gdstore for message metadata persistence
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

	b, err := bot.New(token, bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage, models.AllowedUpdateMessageReaction}))
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

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.MessageReaction != nil
	}, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handleMessageReaction(ctx, b, update.MessageReaction)
	})

	// Catch-all for plain (non-command) messages: handles pending link replies and auto-detected bsky URLs
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
}

func handleStartCommand(ctx context.Context, b *bot.Bot, msg *models.Message) {
	if !(msg.Chat.Type == "private") {
		return
	}

	welcomeText := `👋 Welcome to Bluesky Spoiler Bot!

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

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      welcomeText,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		log.Printf("sending start message: %v", err)
	}
}

func parseCommandArg(text string) string {
	// Strip command (everything up to and including the first word)
	arg := ""
	if idx := indexOf(text, ' '); idx >= 0 {
		arg = text[idx+1:]
	}
	// Strip bot username mention (e.g. @BotName) if present
	if len(arg) > 0 && arg[0] == '@' {
		if idx := indexOf(arg, ' '); idx >= 0 {
			arg = arg[idx+1:]
		} else {
			arg = ""
		}
	}
	return trimSpace(arg)
}

func handleMediaCommand(ctx context.Context, b *bot.Bot, msg *models.Message, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient, hasSpoiler bool) {
	arg := parseCommandArg(msg.Text)

	if utf8.RuneCountInString(arg) == 0 {
		if msg.ReplyToMessage != nil {
			if parsed, err := ParseMediaURL(msg.ReplyToMessage.Text); err == nil {
				var origUser *models.User
				if msg.ReplyToMessage.From != nil {
					origUser = msg.ReplyToMessage.From
				}
				processMediaURL(ctx, b, msg, parsed.OriginalURL, hasSpoiler, bskyClient, inkbunnyClient, origUser)
				return
			}
		}
		cmd := "/spoiler"
		if !hasSpoiler {
			cmd = "/nospoiler"
		}
		askMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text: fmt.Sprintf(
				"Please send me a post link.\n\n"+
					"💡 You can also use the command directly:\n"+
					"<code>%s &lt;URL&gt; [content warning]</code>",
				cmd,
			),
			ParseMode: models.ParseModeHTML,
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		if err != nil {
			log.Printf("sending ask-for-link message: %v", err)
			return
		}
		key := fmt.Sprintf("%d:%d", msg.Chat.ID, msg.From.ID)
		pendingRequests.Store(key, &PendingRequest{
			HasSpoiler: hasSpoiler,
			BotMsgID:   askMsg.ID,
			CmdMsgID:   msg.ID,
		})
		return
	}

	processMediaURL(ctx, b, msg, arg, hasSpoiler, bskyClient, inkbunnyClient, nil)
}

// handlePlainMessage handles non-command messages. It only acts when the user has a pending
// link request (sent a command without a URL). The pending slot is consumed on the first reply,
// valid or not.
func handlePlainMessage(ctx context.Context, b *bot.Bot, msg *models.Message, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient) {
	key := fmt.Sprintf("%d:%d", msg.Chat.ID, msg.From.ID)
	val, ok := pendingRequests.LoadAndDelete(key)
	if !ok {
		return
	}
	pending := val.(*PendingRequest)

	linkText := msg.Text
	var origUser *models.User
	if _, err := ParseMediaURL(linkText); err != nil {
		if msg.ReplyToMessage != nil {
			if _, err2 := ParseMediaURL(msg.ReplyToMessage.Text); err2 == nil {
				linkText = msg.ReplyToMessage.Text
				if msg.ReplyToMessage.From != nil {
					origUser = msg.ReplyToMessage.From
				}
			}
		}
	}
	if _, err := ParseMediaURL(linkText); err != nil {
		// Invalid — send an error that auto-deletes after 10 s, clean up all related messages
		errMsg, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "⚠️ That doesn't look like a valid post URL. This message will self-destruct in 10 seconds.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		deleteSilently(ctx, b, msg.Chat.ID, pending.BotMsgID, pending.CmdMsgID)
		if sendErr == nil && errMsg != nil {
			go func() {
				time.Sleep(10 * time.Second)
				deleteSilently(ctx, b, msg.Chat.ID, errMsg.ID)
			}()
		}
		return
	}

	// Valid URL — clean up the ask and original command messages, then process.
	// The link message (msg) will be deleted by the existing deleteCommandMessage inside processMediaURL.
	deleteSilently(ctx, b, msg.Chat.ID, pending.BotMsgID, pending.CmdMsgID)
	processMediaURL(ctx, b, msg, linkText, pending.HasSpoiler, bskyClient, inkbunnyClient, origUser)
}

func deleteSilently(ctx context.Context, b *bot.Bot, chatID int64, msgIDs ...int) {
	for _, id := range msgIDs {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: id}) //nolint
	}
}

func processMediaURL(ctx context.Context, b *bot.Bot, msg *models.Message, arg string, hasSpoiler bool, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient, origUser *models.User) {
	parsed, err := ParseMediaURL(arg)
	var cwText string
	if err == nil {
		urlIdx := strings.Index(arg, parsed.OriginalURL)
		if urlIdx >= 0 {
			cwText = trimSpace(arg[urlIdx+len(parsed.OriginalURL):])
		}
	}
	if err != nil {
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Please provide a valid post URL (supports bsky.app, x.com, twitter.com, fxtwitter.com, vxtwitter.com, fixupx.com, inkbunny.net, and at:// URIs).",
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
			{
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

	var mediaResult *MediaResult

	if parsed.Source == "inkbunny" {
		mediaResult, err = inkbunnyClient.FetchSubmission(ctx, parsed.Rkey)
		if err != nil {
			log.Printf("fetch inkbunny submission: %v", err)
			if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text:   "Failed to fetch Inkbunny submission.",
				ReplyParameters: &models.ReplyParameters{
					MessageID: msg.ID,
				},
			}); sendErr != nil {
				log.Printf("send error notification: %v", sendErr)
			}
			return
		}
	} else if parsed.Source == "twitter" {
		mediaResult, err = FetchTweet(ctx, parsed.Rkey)
		if err != nil {
			log.Printf("fetch tweet: %v", err)
			if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text:   "Failed to fetch tweet.",
				ReplyParameters: &models.ReplyParameters{
					MessageID: msg.ID,
				},
			}); sendErr != nil {
				log.Printf("send error notification: %v", sendErr)
			}
			return
		}
	} else {
		did, err := bskyClient.ResolveToDID(ctx, parsed.Authority)
		if err != nil {
			log.Printf("resolve DID: %v", err.Error())
			if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text:   "Failed to resolve Bluesky profile.",
				ReplyParameters: &models.ReplyParameters{
					MessageID: msg.ID,
				},
			}); sendErr != nil {
				log.Printf("send error notification: %v", sendErr)
			}
			return
		}

		atURI := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, parsed.Rkey)
		mediaResult, err = bskyClient.FetchPost(ctx, atURI)
		if err != nil {
			log.Printf("fetch post: %v", err)
			if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text:   "Failed to fetch post images.",
				ReplyParameters: &models.ReplyParameters{
					MessageID: msg.ID,
				},
			}); sendErr != nil {
				log.Printf("send error notification: %v", sendErr)
			}
			return
		}
	}

	if len(mediaResult.Images) == 0 && mediaResult.Video == nil {
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "No images or video found in that post.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		})
		if err != nil {
			log.Printf("SendMessage: No images found in that post: %v", err)
		}
		return
	}

	var origFirstName, origUsername string
	if origUser != nil {
		origFirstName = origUser.FirstName
		origUsername = origUser.Username
	}

	var caption string
	if parsed.Source == "inkbunny" {
		caption = buildInkbunnyCaption(msg.From.FirstName, msg.From.Username, origFirstName, origUsername, mediaResult.Title, mediaResult.Author, mediaResult.AuthorURL, mediaResult.SubmissionURL, cwText, mediaResult.Text, hasSpoiler)
	} else {
		caption = buildCaption(msg.From.FirstName, msg.From.Username, origFirstName, origUsername, parsed.OriginalURL, cwText, mediaResult.Text, hasSpoiler)
	}

	if mediaResult.Video != nil {
		referrer := ""
		if parsed.Source == "inkbunny" {
			referrer = "https://inkbunny.net/"
		}
		handleVideoPost(ctx, b, msg, mediaResult.Video, caption, hasSpoiler, referrer)
		return
	}

	handleImagePost(ctx, b, msg, mediaResult.Images, caption, hasSpoiler)
}

func handleImagePost(ctx context.Context, b *bot.Bot, msg *models.Message, images []MediaImage, caption string, hasSpoiler bool) {
	var sentMsgIDs []int

	imagesToSend := images
	pageNote := ""
	if len(images) > 10 {
		pageNote = fmt.Sprintf("\n📄 Showing 10 of %d pages", len(images))
		imagesToSend = images[:10]
	}
	if pageNote != "" {
		caption += pageNote
	}

	if len(imagesToSend) == 1 {
		img := imagesToSend[0]
		var photo models.InputFile
		var localFile *os.File
		if img.NeedsDownload {
			body, err := downloadWithReferrer(ctx, img.Fullsize, "https://inkbunny.net/")
			if err != nil {
				log.Printf("download image: %v", err)
				return
			}
			defer body.Close()
			tmpFile, err := os.CreateTemp("", "ib-img-*.tmp")
			if err != nil {
				log.Printf("create temp file: %v", err)
				return
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath)
			if _, err = io.Copy(tmpFile, body); err != nil {
				tmpFile.Close()
				log.Printf("write temp file: %v", err)
				return
			}
			tmpFile.Close()
			f, err := os.Open(tmpPath)
			if err != nil {
				log.Printf("open temp file: %v", err)
				return
			}
			defer f.Close()
			localFile = f
			photo = &models.InputFileUpload{Filename: "image.jpg", Data: f}
		} else {
			photo = &models.InputFileString{Data: img.Fullsize}
		}
		sentMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:                msg.Chat.ID,
			Photo:                 photo,
			Caption:               caption,
			ParseMode:             models.ParseModeHTML,
			HasSpoiler:            hasSpoiler,
			ShowCaptionAboveMedia: true,
		})
		if err != nil {
			log.Printf("SendPhoto failed, retrying as document: %v", err)
			if localFile != nil {
				if _, seekErr := localFile.Seek(0, 0); seekErr != nil {
					log.Printf("seek temp file: %v", seekErr)
					return
				}
			}
			sentMsg, err = b.SendDocument(ctx, &bot.SendDocumentParams{
				ChatID:    msg.Chat.ID,
				Document:  photo,
				Caption:   caption,
				ParseMode: models.ParseModeHTML,
			})
			if err != nil {
				log.Printf("SendDocument: %v", err)
				return
			}
		}
		sentMsgIDs = []int{sentMsg.ID}
	} else {
		media := make([]models.InputMedia, len(imagesToSend))
		var downloadedFiles []*os.File
		defer func() {
			for _, f := range downloadedFiles {
				f.Close()
			}
		}()
		for i, img := range imagesToSend {
			p := &models.InputMediaPhoto{
				HasSpoiler:            hasSpoiler,
				ShowCaptionAboveMedia: true,
			}
			if img.NeedsDownload {
				body, err := downloadWithReferrer(ctx, img.Fullsize, "https://inkbunny.net/")
				if err != nil {
					log.Printf("download image %d: %v", i, err)
					return
				}
				tmpFile, err := os.CreateTemp("", "ib-img-*.tmp")
				if err != nil {
					body.Close()
					log.Printf("create temp file: %v", err)
					return
				}
				if _, err = io.Copy(tmpFile, body); err != nil {
					body.Close()
					tmpFile.Close()
					log.Printf("write temp file: %v", err)
					return
				}
				body.Close()
				tmpFile.Close()
				f, err := os.Open(tmpFile.Name())
				if err != nil {
					log.Printf("open temp file: %v", err)
					return
				}
				downloadedFiles = append(downloadedFiles, f)
				defer os.Remove(tmpFile.Name())
				p.Media = fmt.Sprintf("attach://img_%d", i)
				p.MediaAttachment = f
			} else {
				p.Media = img.Fullsize
			}
			if i == 0 {
				p.Caption = caption
				p.ParseMode = models.ParseModeHTML
			}
			media[i] = p
		}
		sentMsgs, err := b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
			ChatID: msg.Chat.ID,
			Media:  media,
		})
		if err != nil {
			log.Printf("SendMediaGroup failed, retrying as documents: %v", err)
			for i, img := range imagesToSend {
				var doc models.InputFile
				if img.NeedsDownload && i < len(downloadedFiles) {
					if _, seekErr := downloadedFiles[i].Seek(0, 0); seekErr != nil {
						log.Printf("seek temp file %d: %v", i, seekErr)
						continue
					}
					doc = &models.InputFileUpload{Filename: fmt.Sprintf("image_%d.jpg", i), Data: downloadedFiles[i]}
				} else {
					doc = &models.InputFileString{Data: img.Fullsize}
				}
				docCaption := ""
				if i == 0 {
					docCaption = caption
				}
				sentMsg, sendErr := b.SendDocument(ctx, &bot.SendDocumentParams{
					ChatID:    msg.Chat.ID,
					Document:  doc,
					Caption:   docCaption,
					ParseMode: models.ParseModeHTML,
				})
				if sendErr != nil {
					log.Printf("SendDocument %d: %v", i, sendErr)
					continue
				}
				sentMsgIDs = append(sentMsgIDs, sentMsg.ID)
			}
			if len(sentMsgIDs) == 0 {
				return
			}
		} else {
			for _, sentMsg := range sentMsgs {
				sentMsgIDs = append(sentMsgIDs, sentMsg.ID)
			}
		}
	}

	for _, id := range sentMsgIDs {
		storeMessageMetadata(msg.Chat.ID, id, msg.From.ID)
	}
	deleteCommandMessage(ctx, b, msg)
}

func deleteCommandMessage(ctx context.Context, b *bot.Bot, msg *models.Message) {
	_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
	})
	if err != nil {
		log.Println("Can't delete sender's message, does bot have permission to delete messages?", err)
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text: "Can't delete sender's message, does bot have permission to delete messages?" +
				"```error\n" +
				err.Error() +
				"```",
			ParseMode: models.ParseModeMarkdown,
		}); sendErr != nil {
			log.Printf("send permission error notification: %v", sendErr)
		}
	}
}

func handleVideoPost(ctx context.Context, b *bot.Bot, msg *models.Message, video *MediaVideo, caption string, hasSpoiler bool, referrer string) {
	// Send progress indicator
	progressMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   "⏳ Downloading video...",
		ReplyParameters: &models.ReplyParameters{
			MessageID: msg.ID,
		},
	})
	if err != nil {
		log.Printf("send progress message: %v", err)
	}

	deleteProgress := func() {
		if progressMsg != nil {
			b.DeleteMessage(ctx, &bot.DeleteMessageParams{
				ChatID:    msg.Chat.ID,
				MessageID: progressMsg.ID,
			})
		}
	}

	// Download the raw MP4 blob directly
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, video.DirectURL, nil)
	if err != nil {
		log.Printf("create download request: %v", err)
		deleteProgress()
		return
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("download video: %v", err)
		deleteProgress()
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Failed to download video.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		}); sendErr != nil {
			log.Printf("send error notification: %v", sendErr)
		}
		return
	}
	defer resp.Body.Close()

	tmpFile, err := os.CreateTemp("", "video-*.mp4")
	if err != nil {
		log.Printf("create temp file: %v", err)
		deleteProgress()
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err = io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		log.Printf("write temp file: %v", err)
		deleteProgress()
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Failed to download video.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		}); sendErr != nil {
			log.Printf("send error notification: %v", sendErr)
		}
		return
	}
	tmpFile.Close()

	f, err := os.Open(tmpPath)
	if err != nil {
		log.Printf("open temp file: %v", err)
		deleteProgress()
		return
	}
	defer f.Close()

	sentMsg, err := b.SendVideo(ctx, &bot.SendVideoParams{
		ChatID:                msg.Chat.ID,
		Video:                 &models.InputFileUpload{Filename: "video.mp4", Data: f},
		Caption:               caption,
		ParseMode:             models.ParseModeHTML,
		HasSpoiler:            hasSpoiler,
		ShowCaptionAboveMedia: true,
	})
	if err != nil {
		log.Printf("SendVideo: %v", err)
		deleteProgress()
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Failed to send video.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
		}); sendErr != nil {
			log.Printf("send error notification: %v", sendErr)
		}
		return
	}

	storeMessageMetadata(msg.Chat.ID, sentMsg.ID, msg.From.ID)
	deleteProgress()
	deleteCommandMessage(ctx, b, msg)
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
	if reaction.Chat.Type == "private" || len(reaction.NewReaction) == 0 {
		return
	}

	metadata := getMessageMetadata(reaction.Chat.ID, reaction.MessageID)
	if metadata == nil {
		return
	}

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

	chatTitle := reaction.Chat.Title
	if chatTitle == "" {
		chatTitle = "a chat"
	}

	chatIDStr := fmt.Sprintf("%d", reaction.Chat.ID)
	if len(chatIDStr) > 4 && chatIDStr[:4] == "-100" {
		chatIDStr = chatIDStr[4:]
	}
	messageLink := fmt.Sprintf("https://t.me/c/%s/%d", chatIDStr, reaction.MessageID)

	var emojis []string
	for _, r := range reaction.NewReaction {
		if r.ReactionTypeEmoji != nil {
			emojis = append(emojis, r.ReactionTypeEmoji.Emoji)
		} else if r.ReactionTypeCustomEmoji != nil {
			emojis = append(emojis, "[custom]")
		}
	}

	notificationText := fmt.Sprintf(
		"🎭 <b>%s</b> reacted: %s\n\n"+
			"<b>In:</b> %s\n\n"+
			"<a href=\"%s\">Jump to message</a>",
		reactorName,
		strings.Join(emojis, " "),
		chatTitle,
		messageLink,
	)

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:              metadata.UserID,
		Text:                notificationText,
		ParseMode:           models.ParseModeHTML,
		DisableNotification: true,
	})
	if err != nil {
		log.Printf("send reaction notification to user %d: %v", metadata.UserID, err)
	}
}

func buildCaption(firstName, username, origFirstName, origUsername, originalURL, cwText, postText string, hasSpoiler bool) string {
	var body string
	if origUsername != "" && origUsername != username {
		body = fmt.Sprintf(
			`<a href="%s">%s</a> + <a href="%s">%s</a>`+"\n%s",
			"t.me/"+origUsername,
			"@"+origUsername,
			"t.me/"+username,
			"@"+username,
			originalURL,
		)
	} else {
		body = fmt.Sprintf(
			`<a href="%s">%s</a> (%s) shared:`+"\n%s",
			"t.me/"+username,
			firstName,
			"@"+username,
			originalURL,
		)
	}
	if cwText != "" {
		if hasSpoiler {
			body = fmt.Sprintf("<blockquote><b>CW: %s</b></blockquote>", html.EscapeString(cwText)) + body
		} else {
			body = fmt.Sprintf("<blockquote><b>%s</b></blockquote>", html.EscapeString(cwText)) + body
		}
	}
	if postText != "" {
		body += fmt.Sprintf(
			"\n<blockquote expandable>⠀\n⠀  <b>Show post text (tap)</b>\n⠀ \n\n%s</blockquote>",
			html.EscapeString(postText),
		)
	}
	return body
}

func buildInkbunnyCaption(firstName, username, origFirstName, origUsername, title, author, authorURL, submissionURL, cwText, description string, hasSpoiler bool) string {
	var body string
	workLink := fmt.Sprintf(`<a href="%s">%s</a>`, submissionURL, html.EscapeString(title))
	if origUsername != "" && origUsername != username {
		body = fmt.Sprintf(
			`<a href="%s">%s</a> + <a href="%s">%s</a>`+"\n%s by %s",
			"t.me/"+origUsername,
			"@"+origUsername,
			"t.me/"+username,
			"@"+username,
			workLink,
			fmt.Sprintf(`<a href="%s">%s</a>`, authorURL, html.EscapeString(author)),
		)
	} else {
		body = fmt.Sprintf(
			`<a href="%s">%s</a> (%s) shared:`+"\n\"<b>%s</b>\" by <b>%s</b>",
			"t.me/"+username,
			firstName,
			"@"+username,
			workLink,
			fmt.Sprintf(`<a href="%s">%s</a>`, authorURL, html.EscapeString(author)),
		)
	}
	if cwText != "" {
		if hasSpoiler {
			body = fmt.Sprintf("<blockquote><b>CW: %s</b></blockquote>", html.EscapeString(cwText)) + body
		} else {
			body = fmt.Sprintf("<blockquote><b>%s</b></blockquote>", html.EscapeString(cwText)) + body
		}
	}
	if description != "" {
		body += fmt.Sprintf(
			"\n<blockquote expandable>⠀\n⠀  <b>Show description (tap)</b>\n⠀ \n\n%s</blockquote>",
			description,
		)
	}
	return body
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
