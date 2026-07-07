package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

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

func headRequest(ctx context.Context, fileURL, referrer string) (contentLength int64, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fileURL, nil)
	if err != nil {
		return -1, "", fmt.Errorf("create HEAD request: %w", err)
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return -1, "", fmt.Errorf("HEAD request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return -1, "", fmt.Errorf("HEAD request: status %d", resp.StatusCode)
	}

	cl := resp.ContentLength
	if cl < 0 {
		cl = -1
	}
	ct := resp.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return cl, ct, nil
}

func downloadWithLimit(ctx context.Context, fileURL, referrer, pattern string, maxSize int64) (*os.File, func(), string, error) {
	cl, ct, err := headRequest(ctx, fileURL, referrer)
	if err != nil {
		return nil, nil, "", fmt.Errorf("HEAD check: %w", err)
	}
	if cl > 0 && cl > maxSize {
		return nil, nil, "", fmt.Errorf("file too large: %d bytes exceeds limit of %d bytes", cl, maxSize)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create request: %w", err)
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if ct == "" {
		ct = resp.Header.Get("Content-Type")
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = ct[:i]
		}
	}

	ext := extensionForContentType(ct)
	if ext == "" {
		ext = ".mp4"
	}
	pattern = strings.TrimSuffix(pattern, "*") + "*" + ext

	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()

	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if written+int64(n) > maxSize {
				tmpFile.Close()
				os.Remove(tmpPath)
				return nil, nil, "", fmt.Errorf("download aborted: %d bytes exceeds limit of %d bytes", written+int64(n), maxSize)
			}
			_, writeErr := tmpFile.Write(buf[:n])
			if writeErr != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				return nil, nil, "", fmt.Errorf("write temp: %w", writeErr)
			}
			written += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, nil, "", fmt.Errorf("read response: %w", readErr)
		}
	}
	tmpFile.Close()

	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, nil, "", fmt.Errorf("open temp: %w", err)
	}

	cleanup := func() {
		f.Close()
		os.Remove(tmpPath)
	}
	return f, cleanup, ct, nil
}

func extensionForContentType(ct string) string {
	ext, err := mime.ExtensionsByType(ct)
	if err == nil && len(ext) > 0 {
		switch ct {
		case "video/mp4":
			return ".mp4"
		case "video/webm":
			return ".webm"
		case "video/quicktime":
			return ".mov"
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		default:
			return ext[0]
		}
	}
	if strings.HasPrefix(ct, "video/") {
		return ".mp4"
	}
	if strings.HasPrefix(ct, "image/") {
		return ".jpg"
	}
	return ""
}
