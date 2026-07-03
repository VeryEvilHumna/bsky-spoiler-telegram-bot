package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func handleStartCommand(ctx context.Context, b *bot.Bot, msg *models.Message) {
	if msg.Chat.Type != "private" {
		return
	}

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
	arg := ""
	if idx := strings.IndexByte(text, ' '); idx >= 0 {
		arg = text[idx+1:]
	}
	if len(arg) > 0 && arg[0] == '@' {
		if idx := strings.IndexByte(arg, ' '); idx >= 0 {
			arg = arg[idx+1:]
		} else {
			arg = ""
		}
	}
	return strings.TrimSpace(arg)
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
				processMediaURL(ctx, b, msg, parsed.OriginalURL, hasSpoiler, bskyClient, inkbunnyClient, origUser, true)
				if msg.ReplyToMessage.From != nil && msg.From.ID == msg.ReplyToMessage.From.ID {
					if strings.HasPrefix(strings.TrimSpace(msg.ReplyToMessage.Text), parsed.OriginalURL) {
						deleteSilently(ctx, b, msg.Chat.ID, msg.ReplyToMessage.ID)
					}
				}
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

	processMediaURL(ctx, b, msg, arg, hasSpoiler, bskyClient, inkbunnyClient, nil, true)
}

func handlePlainMessage(ctx context.Context, b *bot.Bot, msg *models.Message, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient) {
	key := fmt.Sprintf("%d:%d", msg.Chat.ID, msg.From.ID)
	val, ok := pendingRequests.LoadAndDelete(key)
	if !ok {
		if msg.From.IsBot {
			return
		}
		parsed, err := ParseMediaURL(msg.Text)
		if err != nil {
			return
		}
		promptMsg, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text: fmt.Sprintf(`
Would you like for me to embed that link?

🫧 This message would dissapear in %d seconds`, promptDeleteDelay/time.Second),
			ParseMode: models.ParseModeHTML,
			ReplyParameters: &models.ReplyParameters{
				MessageID: msg.ID,
			},
			DisableNotification: true,
			ReplyMarkup: models.InlineKeyboardMarkup{
				InlineKeyboard: [][]models.InlineKeyboardButton{
					{
						{Text: "Spoiler", CallbackData: fmt.Sprintf("embed:%d:s", msg.ID)},
						{Text: "No Spoiler", CallbackData: fmt.Sprintf("embed:%d:n", msg.ID)},
					},
				},
			},
		})
		if sendErr != nil {
			log.Printf("sending embed prompt: %v", sendErr)
			return
		}
		pendingEmbeds.Store(msg.ID, &PendingEmbed{
			URL:           parsed.OriginalURL,
			ChatID:        msg.Chat.ID,
			OriginalMsgID: msg.ID,
			UserID:        msg.From.ID,
			UserFirstName: msg.From.FirstName,
			UserUsername:  msg.From.Username,
			MsgText:       msg.Text,
		})
		go func() {
			time.Sleep(promptDeleteDelay)
			deleteSilently(ctx, b, msg.Chat.ID, promptMsg.ID)
			pendingEmbeds.Delete(msg.ID)
		}()
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
				time.Sleep(autoDeleteDelay)
				deleteSilently(ctx, b, msg.Chat.ID, errMsg.ID)
			}()
		}
		return
	}

	deleteSilently(ctx, b, msg.Chat.ID, pending.BotMsgID, pending.CmdMsgID)
	processMediaURL(ctx, b, msg, linkText, pending.HasSpoiler, bskyClient, inkbunnyClient, origUser, true)
}

func processMediaURL(ctx context.Context, b *bot.Bot, msg *models.Message, arg string, hasSpoiler bool, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient, origUser *models.User, deleteOriginal bool) {
	parsed, err := ParseMediaURL(arg)
	var cwText string
	if err == nil {
		urlIdx := strings.Index(arg, parsed.OriginalURL)
		if urlIdx >= 0 {
			cwText = strings.TrimSpace(arg[urlIdx+len(parsed.OriginalURL):])
		}
	}
	if err != nil {
		sendErrorReply(ctx, b, msg, "Please provide a valid post URL (supports bsky.app, x.com, twitter.com, fxtwitter.com, vxtwitter.com, fixupx.com, inkbunny.net, and at:// URIs).")
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
			sendErrorReply(ctx, b, msg, "Failed to fetch Inkbunny submission.")
			return
		}
	} else if parsed.Source == "twitter" {
		mediaResult, err = FetchTweet(ctx, parsed.Rkey)
		if err != nil {
			log.Printf("fetch tweet: %v", err)
			sendErrorReply(ctx, b, msg, "Failed to fetch tweet.")
			return
		}
	} else {
		did, err := bskyClient.ResolveToDID(ctx, parsed.Authority)
		if err != nil {
			log.Printf("resolve DID: %v", err)
			sendErrorReply(ctx, b, msg, "Failed to resolve Bluesky profile.")
			return
		}

		atURI := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, parsed.Rkey)
		mediaResult, err = bskyClient.FetchPost(ctx, atURI)
		if err != nil {
			log.Printf("fetch post: %v", err)
			sendErrorReply(ctx, b, msg, "Failed to fetch post images.")
			return
		}
	}

	if len(mediaResult.Images) == 0 && mediaResult.Video == nil {
		sendErrorReply(ctx, b, msg, "No images or video found in that post.")
		return
	}

	var origFirstName, origUsername string
	if origUser != nil {
		origFirstName = origUser.FirstName
		origUsername = origUser.Username
	}

	caption := buildCaption(CaptionData{
		Source:        parsed.Source,
		FirstName:     msg.From.FirstName,
		Username:      msg.From.Username,
		OrigFirstName: origFirstName,
		OrigUsername:  origUsername,
		CWText:        cwText,
		HasSpoiler:    hasSpoiler,
		PostText:      mediaResult.Text,
		TextIsHTML:    mediaResult.TextIsHTML,
		OriginalURL:   parsed.OriginalURL,
		Title:         mediaResult.Title,
		Author:        mediaResult.Author,
		AuthorURL:     mediaResult.AuthorURL,
		SubmissionURL: mediaResult.SubmissionURL,
	})

	if mediaResult.Video != nil {
		referrer := ""
		if parsed.Source == "inkbunny" {
			referrer = inkbunnyReferrer
		}
		handleVideoPost(ctx, b, msg, mediaResult.Video, caption, hasSpoiler, referrer, deleteOriginal)
		return
	}

	handleImagePost(ctx, b, msg, mediaResult.Images, caption, hasSpoiler, deleteOriginal)
}

func handleImagePost(ctx context.Context, b *bot.Bot, msg *models.Message, images []MediaImage, caption string, hasSpoiler bool, deleteOriginal bool) {
	var sentMsgIDs []int

	imagesToSend := images
	pageNote := ""
	if len(images) > maxImagesPerPost {
		pageNote = fmt.Sprintf("\n📄 Showing %d of %d pages", maxImagesPerPost, len(images))
		imagesToSend = images[:maxImagesPerPost]
	}
	if pageNote != "" {
		caption += pageNote
	}

	if len(imagesToSend) == 1 {
		img := imagesToSend[0]
		var photo models.InputFile
		var localFile *os.File
		if img.NeedsDownload {
			f, cleanup, err := downloadToTempFile(ctx, img.Fullsize, inkbunnyReferrer, "ib-img-*.tmp")
			if err != nil {
				log.Printf("download image: %v", err)
				return
			}
			defer cleanup()
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
		var cleanups []func()
		defer func() {
			for _, c := range cleanups {
				c()
			}
		}()
		for i, img := range imagesToSend {
			p := &models.InputMediaPhoto{
				HasSpoiler:            hasSpoiler,
				ShowCaptionAboveMedia: true,
			}
			if img.NeedsDownload {
				f, cleanup, err := downloadToTempFile(ctx, img.Fullsize, inkbunnyReferrer, "ib-img-*.tmp")
				if err != nil {
					log.Printf("download image %d: %v", i, err)
					return
				}
				downloadedFiles = append(downloadedFiles, f)
				cleanups = append(cleanups, cleanup)
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
	if deleteOriginal {
		deleteCommandMessage(ctx, b, msg)
	}
}

func handleVideoPost(ctx context.Context, b *bot.Bot, msg *models.Message, video *MediaVideo, caption string, hasSpoiler bool, referrer string, deleteOriginal bool) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, video.DirectURL, nil)
	if err != nil {
		log.Printf("create download request: %v", err)
		deleteProgress()
		return
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("download video: %v", err)
		deleteProgress()
		sendErrorReply(ctx, b, msg, "Failed to download video.")
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
		sendErrorReply(ctx, b, msg, "Failed to download video.")
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
		sendErrorReply(ctx, b, msg, "Failed to send video.")
		return
	}

	storeMessageMetadata(msg.Chat.ID, sentMsg.ID, msg.From.ID)
	deleteProgress()
	if deleteOriginal {
		deleteCommandMessage(ctx, b, msg)
	}
}

func handleEmbedCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	parts := strings.SplitN(cb.Data, ":", 3)
	if len(parts) != 3 {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Invalid request.",
		})
		return
	}
	originalMsgID, err := strconv.Atoi(parts[1])
	if err != nil {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Invalid request.",
		})
		return
	}
	hasSpoiler := parts[2] == "s"

	val, ok := pendingEmbeds.LoadAndDelete(originalMsgID)
	if !ok {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Link expired, send the URL again.",
			ShowAlert:       true,
		})
		return
	}
	embed := val.(*PendingEmbed)

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
	})

	var promptChatID int64
	var promptMsgID int
	switch cb.Message.Type {
	case models.MaybeInaccessibleMessageTypeMessage:
		promptChatID = cb.Message.Message.Chat.ID
		promptMsgID = cb.Message.Message.ID
	case models.MaybeInaccessibleMessageTypeInaccessibleMessage:
		promptChatID = cb.Message.InaccessibleMessage.Chat.ID
		promptMsgID = cb.Message.InaccessibleMessage.MessageID
	}

	b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    promptChatID,
		MessageID: promptMsgID,
	})

	deleteOriginal := cb.From.ID == embed.UserID && strings.TrimSpace(embed.MsgText) == embed.URL

	senderMsg := &models.Message{
		ID:   embed.OriginalMsgID,
		Chat: models.Chat{ID: embed.ChatID},
		From: &models.User{
			ID:        embed.UserID,
			FirstName: embed.UserFirstName,
			Username:  embed.UserUsername,
		},
	}

	bskyClient := NewBlueskyClient()
	inkbunnyClient := NewInkbunnyClient(os.Getenv("INKBUNNY_USERNAME"), os.Getenv("INKBUNNY_PASSWORD"))
	processMediaURL(ctx, b, senderMsg, embed.URL, hasSpoiler, bskyClient, inkbunnyClient, nil, deleteOriginal)
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

	chatID := reaction.Chat.ID
	if chatID < 0 {
		chatID = -chatID - telegramGroupIDBase
	}
	messageLink := fmt.Sprintf("https://t.me/c/%d/%d", chatID, reaction.MessageID)

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
