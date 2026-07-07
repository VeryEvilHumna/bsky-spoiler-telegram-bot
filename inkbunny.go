package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var inkbunnyURLRegex = regexp.MustCompile(`https?://inkbunny\.net/s/(\d+)(?:\?[^#\s]*)?(?:#[^\s]*)?`)

type InkbunnyClient struct {
	username string
	password string
	sid      string
	mu       sync.Mutex
	http     *http.Client
}

func NewInkbunnyClient(username, password string) *InkbunnyClient {
	return &InkbunnyClient{
		username: username,
		password: password,
		http:     httpClient,
	}
}

func ParseInkbunnyURL(text string) (*ParsedMediaURL, error) {
	m := inkbunnyURLRegex.FindStringSubmatch(text)
	if m == nil {
		return nil, fmt.Errorf("no Inkbunny URL found")
	}
	return &ParsedMediaURL{
		Source:      "inkbunny",
		Authority:   "",
		Rkey:        m[1],
		OriginalURL: m[0],
	}, nil
}

func (c *InkbunnyClient) ensureSID(ctx context.Context) error {
	if c.sid != "" {
		return nil
	}
	form := url.Values{}
	if c.username != "" {
		form.Set("username", c.username)
		form.Set("password", c.password)
	} else {
		form.Set("username", "guest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://inkbunny.net/api_login.php", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read login response: %w", err)
	}

	var result struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse login response: %w", err)
	}
	if result.SID == "" {
		return fmt.Errorf("no SID in login response: %s", string(body))
	}
	c.sid = result.SID
	return nil
}

func (c *InkbunnyClient) FetchSubmission(ctx context.Context, submissionID string) (*MediaResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureSID(ctx); err != nil {
		return nil, err
	}

	data, err := c.fetchSubmissionData(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	if isSIDError(data) {
		c.sid = ""
		if err := c.ensureSID(ctx); err != nil {
			return nil, err
		}
		data, err = c.fetchSubmissionData(ctx, submissionID)
		if err != nil {
			return nil, err
		}
	}

	return c.parseSubmissionResponse(data)
}

func (c *InkbunnyClient) fetchSubmissionData(ctx context.Context, submissionID string) ([]byte, error) {
	reqURL := fmt.Sprintf("https://inkbunny.net/api_submissions.php?sid=%s&submission_ids=%s&show_description=yes", c.sid, submissionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch submission: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return body, nil
}

func isSIDError(data []byte) bool {
	var errResp struct {
		ErrorCode int `json:"error_code"`
	}
	if err := json.Unmarshal(data, &errResp); err != nil {
		log.Printf("isSIDError: failed to parse response: %v", err)
		return false
	}
	return errResp.ErrorCode == 2
}

func (c *InkbunnyClient) parseSubmissionResponse(data []byte) (*MediaResult, error) {
	var resp struct {
		Submissions []struct {
			SubmissionID string `json:"submission_id"`
			Title        string `json:"title"`
			Username     string `json:"username"`
			Description  string `json:"description"`
			Files        []struct {
				FileURLFull        string `json:"file_url_full"`
				ThumbnailURLMedium string `json:"thumbnail_url_medium"`
				Mimetype           string `json:"mimetype"`
			} `json:"files"`
		} `json:"submissions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse submission: %w", err)
	}
	if len(resp.Submissions) == 0 {
		return nil, fmt.Errorf("submission not found")
	}

	sub := resp.Submissions[0]
	result := &MediaResult{
		Text:          ConvertBBCodeToTelegram(sub.Description),
		Title:         sub.Title,
		Author:        sub.Username,
		AuthorURL:     fmt.Sprintf("https://inkbunny.net/%s", sub.Username),
		SubmissionURL: fmt.Sprintf("https://inkbunny.net/s/%s", sub.SubmissionID),
	}

	for _, f := range sub.Files {
		if f.FileURLFull == "" {
			continue
		}
		if strings.HasPrefix(f.Mimetype, "video/") {
			if result.Video == nil {
				result.Video = &MediaVideo{
					DirectURL:    f.FileURLFull,
					ThumbnailURL: f.ThumbnailURLMedium,
					Variants:     []VideoVariant{{URL: f.FileURLFull}},
				}
			}
		} else {
			result.Images = append(result.Images, MediaImage{
				Fullsize:      f.FileURLFull,
				Thumb:         f.ThumbnailURLMedium,
				NeedsDownload: true,
			})
		}
	}

	return result, nil
}
