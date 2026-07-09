package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func ptrBool(v bool) *bool       { return &v }
func ptrString(v string) *string { return &v }

func sendImageLinkFallback(ctx context.Context, b *bot.Bot, msg *models.Message, imageURL string, caption string, sizeMB float64, limitMB int) {
	text := fmt.Sprintf(
		"⚠️ Image is too large to upload (%.1f MB, max %d MB). Here's a link instead:\n\n%s",
		sizeMB, limitMB, imageURL,
	)
	if caption != "" {
		text = caption + "\n\n" + text
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			URL:              ptrString(imageURL),
			PreferLargeMedia: ptrBool(true),
		},
		ReplyParameters: &models.ReplyParameters{
			MessageID: msg.ID,
		},
	}); err != nil {
		log.Printf("send image link fallback: %v", err)
	}
}

func sizeLimitForImage(img MediaImage) int64 {
	if img.NeedsDownload {
		return telegramMaxUploadSize
	}
	return telegramMaxPhotoSize
}

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
				processMediaURL(ctx, b, msg, parsed.OriginalURL, hasSpoiler, bskyClient, inkbunnyClient, origUser, true, 0)
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

	processMediaURL(ctx, b, msg, arg, hasSpoiler, bskyClient, inkbunnyClient, nil, true, 0)
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

🫧 This message would disappear in %d seconds`, promptDeleteDelay/time.Second),
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
		var origFirstName, origUsername string
		if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
			origFirstName = msg.ReplyToMessage.From.FirstName
			origUsername = msg.ReplyToMessage.From.Username
		}
		pendingEmbeds.Store(msg.ID, &PendingEmbed{
			URL:           parsed.OriginalURL,
			ChatID:        msg.Chat.ID,
			OriginalMsgID: msg.ID,
			UserID:        msg.From.ID,
			UserFirstName: msg.From.FirstName,
			UserUsername:  msg.From.Username,
			MsgText:       msg.Text,
			OrigFirstName: origFirstName,
			OrigUsername:  origUsername,
		})
		go func() {
			time.Sleep(promptDeleteDelay)
			if _, loaded := pendingEmbeds.LoadAndDelete(msg.ID); loaded {
				deleteSilently(ctx, b, msg.Chat.ID, promptMsg.ID)
			}
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
	processMediaURL(ctx, b, msg, linkText, pending.HasSpoiler, bskyClient, inkbunnyClient, origUser, true, 0)
}

func processMediaURL(ctx context.Context, b *bot.Bot, msg *models.Message, arg string, hasSpoiler bool, bskyClient *BlueskyClient, inkbunnyClient *InkbunnyClient, origUser *models.User, deleteOriginal bool, replyMsgID int) {
	parsed, err := ParseMediaURL(arg)
	var cwText string
	if err == nil {
		urlIdx := strings.Index(arg, parsed.OriginalURL)
		if urlIdx >= 0 {
			cwText = strings.TrimSpace(arg[urlIdx+len(parsed.OriginalURL):])
		}
	}
	if err != nil {
		sendErrorReply(ctx, b, msg, "Please provide a valid post URL (supports bsky.app, x.com, twitter.com, fxtwitter.com, vxtwitter.com, fixupx.com, inkbunny.net, instagram.com, and at:// URIs).")
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
	} else if parsed.Source == "instagram" {
		mediaResult, err = FetchInstagramMedia(ctx, parsed.OriginalURL)
		if err != nil {
			log.Printf("fetch instagram: %v", err)
			sendErrorReply(ctx, b, msg, igUserMessage(err))
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
		handleVideoPost(ctx, b, msg, mediaResult.Video, caption, hasSpoiler, referrer, deleteOriginal, replyMsgID)
		return
	}

	handleImagePost(ctx, b, msg, mediaResult.Images, caption, hasSpoiler, deleteOriginal, replyMsgID)
}

func handleImagePost(ctx context.Context, b *bot.Bot, msg *models.Message, images []MediaImage, caption string, hasSpoiler bool, deleteOriginal bool, replyMsgID int) {
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
		referrer := ""
		if img.NeedsDownload {
			referrer = inkbunnyReferrer
		}
		sizeLimit := sizeLimitForImage(img)

		cl, _, headErr := headRequest(ctx, img.Fullsize, referrer)
		if headErr != nil {
			log.Printf("HEAD image: %v", headErr)
		}
		if cl > 0 && cl > sizeLimit {
			sizeMB := float64(cl) / (1024 * 1024)
			sendImageLinkFallback(ctx, b, msg, img.Fullsize, caption, sizeMB, int(sizeLimit/(1024*1024)))
			if deleteOriginal {
				deleteCommandMessage(ctx, b, msg)
			}
			return
		}

		var photo models.InputFile
		var localFile *os.File
		if img.NeedsDownload {
			f, cleanup, err := downloadToTempFile(ctx, img.Fullsize, inkbunnyReferrer, "ib-img-*.tmp")
			if err != nil {
				log.Printf("download image: %v", err)
				sendErrorReply(ctx, b, msg, fmt.Sprintf("Failed to download image: %v", err))
				return
			}
			defer cleanup()
			localFile = f
			photo = &models.InputFileUpload{Filename: "image.jpg", Data: f}
		} else {
			photo = &models.InputFileString{Data: img.Fullsize}
		}
		sendPhotoParams := &bot.SendPhotoParams{
			ChatID:                msg.Chat.ID,
			Photo:                 photo,
			Caption:               caption,
			ParseMode:             models.ParseModeHTML,
			HasSpoiler:            hasSpoiler,
			ShowCaptionAboveMedia: true,
		}
		if replyMsgID > 0 {
			sendPhotoParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
		}
		sentMsg, err := b.SendPhoto(ctx, sendPhotoParams)
		if err != nil {
			log.Printf("SendPhoto failed, retrying as document: %v", err)
			if localFile != nil {
				if _, seekErr := localFile.Seek(0, 0); seekErr != nil {
					log.Printf("seek temp file: %v", seekErr)
					if !img.NeedsDownload && cl > 0 {
						sizeMB := float64(cl) / (1024 * 1024)
						sendImageLinkFallback(ctx, b, msg, img.Fullsize, caption, sizeMB, int(sizeLimit/(1024*1024)))
					} else {
						sendErrorReply(ctx, b, msg, fmt.Sprintf("Failed to send image: %v", err))
					}
					return
				}
			}
			sendDocFallbackParams := &bot.SendDocumentParams{
				ChatID:    msg.Chat.ID,
				Document:  photo,
				Caption:   caption,
				ParseMode: models.ParseModeHTML,
			}
			if replyMsgID > 0 {
				sendDocFallbackParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
			}
			sentMsg, err = b.SendDocument(ctx, sendDocFallbackParams)
			if err != nil {
				log.Printf("SendDocument: %v", err)
				if !img.NeedsDownload && cl > 0 {
					sizeMB := float64(cl) / (1024 * 1024)
					sendImageLinkFallback(ctx, b, msg, img.Fullsize, caption, sizeMB, int(sizeLimit/(1024*1024)))
				} else {
					sendErrorReply(ctx, b, msg, fmt.Sprintf("Failed to send image: %v", err))
				}
				return
			}
		}
		sentMsgIDs = []int{sentMsg.ID}
	} else {
		media := make([]models.InputMedia, 0, len(imagesToSend))
		downloadedByIndex := make(map[int]*os.File)
		var cleanups []func()
		defer func() {
			for _, c := range cleanups {
				c()
			}
		}()

		tooLargeCount := 0
		downloadFailCount := 0
		for i, img := range imagesToSend {
			referrer := ""
			if img.NeedsDownload {
				referrer = inkbunnyReferrer
			}
			sizeLimit := sizeLimitForImage(img)
			cl, _, headErr := headRequest(ctx, img.Fullsize, referrer)
			if headErr != nil {
				log.Printf("HEAD image %d: %v", i, headErr)
			}
			if cl > 0 && cl > sizeLimit {
				sizeMB := float64(cl) / (1024 * 1024)
				log.Printf("skipping image %d (%.1f MB exceeds limit)", i, sizeMB)
				tooLargeCount++
				continue
			}

			p := &models.InputMediaPhoto{
				HasSpoiler:            hasSpoiler,
				ShowCaptionAboveMedia: true,
			}
			if img.NeedsDownload {
				f, cleanup, err := downloadToTempFile(ctx, img.Fullsize, inkbunnyReferrer, "ib-img-*.tmp")
				if err != nil {
					log.Printf("download image %d: %v", i, err)
					downloadFailCount++
					continue
				}
				downloadedByIndex[i] = f
				cleanups = append(cleanups, cleanup)
				p.Media = fmt.Sprintf("attach://img_%d", i)
				p.MediaAttachment = f
			} else {
				p.Media = img.Fullsize
			}
			if len(media) == 0 {
				p.Caption = caption
				p.ParseMode = models.ParseModeHTML
			}
			media = append(media, p)
		}

		if tooLargeCount > 0 || downloadFailCount > 0 {
			var parts []string
			if tooLargeCount > 0 {
				parts = append(parts, fmt.Sprintf("%d too large", tooLargeCount))
			}
			if downloadFailCount > 0 {
				parts = append(parts, fmt.Sprintf("%d failed to download", downloadFailCount))
			}
			warnMsg := fmt.Sprintf("⚠️ Skipped %s image(s).", strings.Join(parts, ", "))
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text:   warnMsg,
				ReplyParameters: &models.ReplyParameters{
					MessageID: msg.ID,
				},
			})
		}

		if len(media) == 0 {
			sendErrorReply(ctx, b, msg, "No images could be sent (all were too large or failed to download).")
			return
		}

		sendMediaGroupParams := &bot.SendMediaGroupParams{
			ChatID: msg.Chat.ID,
			Media:  media,
		}
		if replyMsgID > 0 {
			sendMediaGroupParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
		}
		sentMsgs, err := b.SendMediaGroup(ctx, sendMediaGroupParams)
		if err != nil {
			log.Printf("SendMediaGroup failed, retrying as documents: %v", err)
			for i, img := range imagesToSend {
				referrer := ""
				if img.NeedsDownload {
					referrer = inkbunnyReferrer
				}
				sizeLimit := sizeLimitForImage(img)
				cl, _, _ := headRequest(ctx, img.Fullsize, referrer)
				if cl > 0 && cl > sizeLimit {
					continue
				}

				var doc models.InputFile
				if f, ok := downloadedByIndex[i]; ok {
					if _, seekErr := f.Seek(0, 0); seekErr != nil {
						log.Printf("seek temp file %d: %v", i, seekErr)
						continue
					}
					doc = &models.InputFileUpload{Filename: fmt.Sprintf("image_%d.jpg", i), Data: f}
				} else if img.NeedsDownload {
					continue
				} else {
					doc = &models.InputFileString{Data: img.Fullsize}
				}
				docCaption := ""
				if len(sentMsgIDs) == 0 {
					docCaption = caption
				}
				sendDocParams := &bot.SendDocumentParams{
					ChatID:    msg.Chat.ID,
					Document:  doc,
					Caption:   docCaption,
					ParseMode: models.ParseModeHTML,
				}
				if replyMsgID > 0 && len(sentMsgIDs) == 0 {
					sendDocParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
				}
				sentMsg, sendErr := b.SendDocument(ctx, sendDocParams)
				if sendErr != nil {
					log.Printf("SendDocument %d: %v", i, sendErr)
					continue
				}
				sentMsgIDs = append(sentMsgIDs, sentMsg.ID)
			}
			if len(sentMsgIDs) == 0 {
				sendErrorReply(ctx, b, msg, "Failed to send any images.")
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

func handleVideoPost(ctx context.Context, b *bot.Bot, msg *models.Message, video *MediaVideo, caption string, hasSpoiler bool, referrer string, deleteOriginal bool, replyMsgID int) {
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

	variants := video.Variants
	if len(variants) == 0 {
		variants = []VideoVariant{{URL: video.DirectURL}}
	}

	var tmpFile *os.File
	var cleanup func()
	var contentType string
	tooLargeCount := 0
	otherFailCount := 0

	for i, v := range variants {
		cl, _, headErr := headRequest(ctx, v.URL, referrer)
		if headErr != nil {
			log.Printf("HEAD %s: %v", v.URL, headErr)
			otherFailCount++
			continue
		}
		if cl > 0 && cl > telegramMaxUploadSize {
			log.Printf("skipping variant %d (%d bytes exceeds %d limit)", i, cl, telegramMaxUploadSize)
			tooLargeCount++
			continue
		}

		f, dlCleanup, ct, dlErr := downloadWithLimit(ctx, v.URL, referrer, "video-*", telegramMaxUploadSize)
		if dlErr != nil {
			log.Printf("download variant %d: %v", i, dlErr)
			if strings.Contains(dlErr.Error(), "exceeds limit") {
				tooLargeCount++
			} else {
				otherFailCount++
			}
			continue
		}

		tmpFile = f
		cleanup = dlCleanup
		contentType = ct
		break
	}

	if tmpFile == nil {
		deleteProgress()
		switch {
		case tooLargeCount > 0 && otherFailCount == 0:
			sendErrorReply(ctx, b, msg, fmt.Sprintf(
				"Video is too large to send via Telegram (max %d MB). Tried %d quality level(s); all exceeded the limit.",
				telegramMaxUploadSize/(1024*1024), len(variants),
			))
		case otherFailCount > 0 && tooLargeCount == 0:
			sendErrorReply(ctx, b, msg, fmt.Sprintf(
				"Failed to download video after trying %d quality level(s). The source may be temporarily unavailable.",
				len(variants),
			))
		default:
			sendErrorReply(ctx, b, msg, fmt.Sprintf(
				"Could not send video (%d quality level(s) too large, %d failed to download). Max upload size is %d MB.",
				tooLargeCount, otherFailCount, telegramMaxUploadSize/(1024*1024),
			))
		}
		return
	}
	defer cleanup()

	ext := extensionForContentType(contentType)
	if ext == "" {
		ext = ".mp4"
	}

	sendVideoParams := &bot.SendVideoParams{
		ChatID:                msg.Chat.ID,
		Video:                 &models.InputFileUpload{Filename: "video" + ext, Data: tmpFile},
		Caption:               caption,
		ParseMode:             models.ParseModeHTML,
		HasSpoiler:            hasSpoiler,
		SupportsStreaming:     true,
		ShowCaptionAboveMedia: true,
	}
	if replyMsgID > 0 {
		sendVideoParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
	}
	sentMsg, err := b.SendVideo(ctx, sendVideoParams)
	if err != nil {
		log.Printf("SendVideo failed, retrying as document: %v", err)
		if _, seekErr := tmpFile.Seek(0, 0); seekErr != nil {
			log.Printf("seek temp file: %v", seekErr)
			deleteProgress()
			sendErrorReply(ctx, b, msg, fmt.Sprintf("Telegram rejected the video: %v", err))
			return
		}
		sendDocFallbackParams := &bot.SendDocumentParams{
			ChatID:    msg.Chat.ID,
			Document:  &models.InputFileUpload{Filename: "video" + ext, Data: tmpFile},
			Caption:   caption,
			ParseMode: models.ParseModeHTML,
		}
		if replyMsgID > 0 {
			sendDocFallbackParams.ReplyParameters = &models.ReplyParameters{MessageID: replyMsgID}
		}
		sentMsg, err = b.SendDocument(ctx, sendDocFallbackParams)
		if err != nil {
			log.Printf("SendDocument: %v", err)
			deleteProgress()
			sendErrorReply(ctx, b, msg, fmt.Sprintf("Telegram rejected the video (tried as document too): %v", err))
			return
		}
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

	shouldDelete := cb.From.ID == embed.UserID && strings.HasPrefix(strings.TrimSpace(embed.MsgText), strings.TrimSpace(embed.URL))

	senderMsg := &models.Message{
		ID:   embed.OriginalMsgID,
		Chat: models.Chat{ID: embed.ChatID},
		From: &models.User{
			ID:        embed.UserID,
			FirstName: embed.UserFirstName,
			Username:  embed.UserUsername,
		},
	}

	replyMsgID := embed.OriginalMsgID
	if shouldDelete {
		replyMsgID = 0
	}

	var origUser *models.User
	if embed.OrigFirstName != "" || embed.OrigUsername != "" {
		origUser = &models.User{
			FirstName: embed.OrigFirstName,
			Username:  embed.OrigUsername,
		}
	}

	bskyClient := NewBlueskyClient()
	inkbunnyClient := NewInkbunnyClient(os.Getenv("INKBUNNY_USERNAME"), os.Getenv("INKBUNNY_PASSWORD"))
	processMediaURL(ctx, b, senderMsg, embed.MsgText, hasSpoiler, bskyClient, inkbunnyClient, origUser, shouldDelete, replyMsgID)
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
