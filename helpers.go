package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var httpClient = &http.Client{Timeout: httpTimeout}

func sendErrorReply(ctx context.Context, b *bot.Bot, msg *models.Message, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	}); err != nil {
		log.Printf("send error reply: %v", err)
	}
}

func deleteSilently(ctx context.Context, b *bot.Bot, chatID int64, msgIDs ...int) {
	for _, id := range msgIDs {
		if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: id}); err != nil {
			log.Printf("deleteSilently: failed to delete message %d in chat %d: %v", id, chatID, err)
		}
	}
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

func downloadWithReferrer(ctx context.Context, fileURL, referrer string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// downloadToTempFile downloads a URL to a temp file and returns the opened file and a cleanup func.
// Caller MUST call cleanup() when done.
func downloadToTempFile(ctx context.Context, url, referrer, pattern string) (*os.File, func(), error) {
	body, err := downloadWithReferrer(ctx, url, referrer)
	if err != nil {
		return nil, nil, fmt.Errorf("download: %w", err)
	}
	defer body.Close()

	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err = io.Copy(tmpFile, body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, nil, fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, nil, fmt.Errorf("open temp: %w", err)
	}

	cleanup := func() {
		f.Close()
		os.Remove(tmpPath)
	}
	return f, cleanup, nil
}
