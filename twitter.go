package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

var twitterURLRegex = regexp.MustCompile(`https?://(?:x|twitter|fxtwitter|vxtwitter|fixupx)\.com/([a-zA-Z0-9_]+)/status/(\d+)`)

func ParseTwitterURL(text string) (*ParsedMediaURL, error) {
	m := twitterURLRegex.FindStringSubmatch(text)
	if m == nil {
		return nil, fmt.Errorf("no Twitter/X URL found")
	}
	return &ParsedMediaURL{
		Source:      "twitter",
		Authority:   m[1],
		Rkey:        m[2],
		OriginalURL: m[0],
	}, nil
}

func FetchTweet(ctx context.Context, tweetID string) (*MediaResult, error) {
	reqURL := fmt.Sprintf("https://api.fxtwitter.com/2/status/%s", tweetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tweet: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch tweet: status %d: %s", resp.StatusCode, string(body))
	}

	return parseTweetResponse(body)
}

func parseTweetResponse(data []byte) (*MediaResult, error) {
	var resp struct {
		Status struct {
			ID   string `json:"id"`
			Text string `json:"text"`
			URL  string `json:"url"`
			Media struct {
				Photos []struct {
					URL    string `json:"url"`
					Width  int    `json:"width"`
					Height int    `json:"height"`
				} `json:"photos"`
				Videos []struct {
					URL          string `json:"url"`
					ThumbnailURL string `json:"thumbnail_url"`
					Width        int    `json:"width"`
					Height       int    `json:"height"`
				} `json:"videos"`
			} `json:"media"`
			Author struct {
				Name       string `json:"name"`
				ScreenName string `json:"screen_name"`
				URL        string `json:"url"`
			} `json:"author"`
		} `json:"status"`
		Code int `json:"code"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse tweet: %w", err)
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("fxtwitter API error: code %d", resp.Code)
	}

	status := resp.Status
	result := &MediaResult{
		Text:         status.Text,
		Author:       status.Author.Name,
		AuthorURL:    status.Author.URL,
		SubmissionURL: status.URL,
	}

	if len(status.Media.Videos) > 0 {
		v := status.Media.Videos[0]
		result.Video = &MediaVideo{
			DirectURL:    v.URL,
			ThumbnailURL: v.ThumbnailURL,
		}
	}

	for _, p := range status.Media.Photos {
		result.Images = append(result.Images, MediaImage{
			Fullsize: p.URL,
			Thumb:    p.URL,
		})
	}

	return result, nil
}
