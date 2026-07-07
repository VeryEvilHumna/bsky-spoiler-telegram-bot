package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	igUserAgent = "Instagram 275.0.0.27.98 Android (33/13; 280dpi; 720x1423; Xiaomi; Redmi 7; onclite; qcom; en_US; 458229237)"
	igAppID     = "936619743392459"
	// igGenericUA is a generic modern Chrome user-agent used for HTML embed
	// and GraphQL requests.
	igGenericUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	// igGraphQLDocID is the doc_id for PolarisPostActionLoadPostQueryQuery.
	igGraphQLDocID = "8845758582119845"
)

var (
	igHTTPClient = &http.Client{Timeout: httpTimeout}

	igPostRegex     = regexp.MustCompile(`https?://(?:www\.)?(?:instagram\.com|ddinstagram\.com)/(?:([A-Za-z0-9._]+)/)?(?:p|reels?|tv)/([A-Za-z0-9_-]+)`)
	igStoryRegex    = regexp.MustCompile(`https?://(?:www\.)?instagram\.com/stories/([A-Za-z0-9._]+)/(\d+)`)
	igShareRegex    = regexp.MustCompile(`https?://(?:www\.)?instagram\.com/share/(?:p/|reel/)?([A-Za-z0-9_-]+)`)
	igCanonicalRegex = regexp.MustCompile(`https://www\.instagram\.com/(?:p|reels?|tv)/[A-Za-z0-9_-]+`)

	ogImageRegex       = regexp.MustCompile(`<meta\s+property="og:image"\s+content="([^"]+)"`)
	ogVideoRegex       = regexp.MustCompile(`<meta\s+property="og:video(?:[^"]*)"\s+content="([^"]+)"`)
	ogDescriptionRegex = regexp.MustCompile(`<meta\s+property="og:description"\s+content="([^"]+)"`)

	// embed contextJSON regex — parse pattern for the embed page.
	igEmbedInitRegex = regexp.MustCompile(`"init",\[\],\[(.*?)\]\],`)

	// GraphQL HTML-scraping regexes (getObjectFromEntries pattern).
	igRESiteData          = regexp.MustCompile(`\["SiteData",.*?,({.*?}),\d+\]`)
	igREPolarisSiteData   = regexp.MustCompile(`\["PolarisSiteData",.*?,({.*?}),\d+\]`)
	igREDGWWebConfig      = regexp.MustCompile(`\["DGWWebConfig",.*?,({.*?}),\d+\]`)
	igREPushInfo          = regexp.MustCompile(`\["InstagramWebPushInfo",.*?,({.*?}),\d+\]`)
	igRELSD               = regexp.MustCompile(`\["LSD",.*?,({.*?}),\d+\]`)
	igRESecurityConfig    = regexp.MustCompile(`\["InstagramSecurityConfig",.*?,({.*?}),\d+\]`)
	igREBloksVersioningID = regexp.MustCompile(`\["WebBloksVersioningID",.*?,({.*?}),\d+\]`)
	igRECometReq          = regexp.MustCompile(`__comet_req=(\d+)`)
	igREJazoest           = regexp.MustCompile(`jazoest=(\d+)`)
)

// igMobileHeaders returns the full mobile API header set.
// Pass cookie="" for anonymous requests.
func igMobileHeaders(cookie string) http.Header {
	h := http.Header{
		"User-Agent":           {igUserAgent},
		"x-ig-app-locale":      {"en_US"},
		"x-ig-device-locale":    {"en_US"},
		"x-ig-mapped-locale":   {"en_US"},
		"accept-language":       {"en-US"},
		"x-fb-http-engine":      {"Liger"},
		"x-fb-client-ip":        {"True"},
		"x-fb-server-cluster":   {"True"},
		"content-length":        {"0"},
		"X-IG-App-ID":           {igAppID},
	}
	if cookie != "" {
		h.Set("Cookie", cookie)
	}
	return h
}

// igEmbedHeaders returns the browser-like header set used for the embed
// page and the GraphQL token-scraping HTML fetch.
func igEmbedHeaders(cookie string) http.Header {
	h := http.Header{
		"Accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"Accept-Language":           {"en-GB,en;q=0.9"},
		"Cache-Control":             {"max-age=0"},
		"Dnt":                       {"1"},
		"Priority":                  {"u=0, i"},
		"Sec-Ch-Ua":                 {`Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`},
		"Sec-Ch-Ua-Mobile":          {"?0"},
		"Sec-Ch-Ua-Platform":        {"macOS"},
		"Sec-Fetch-Dest":            {"document"},
		"Sec-Fetch-Mode":            {"navigate"},
		"Sec-Fetch-Site":            {"none"},
		"Sec-Fetch-User":            {"?1"},
		"Upgrade-Insecure-Requests": {"1"},
		"User-Agent":                {igGenericUA},
		"X-IG-App-ID":                {igAppID},
	}
	if cookie != "" {
		h.Set("Cookie", cookie)
	}
	return h
}

type igError struct {
	Code int
	Msg  string
}

func (e *igError) Error() string { return fmt.Sprintf("instagram: %d: %s", e.Code, e.Msg) }

func igUserMessage(err error) string {
	var igErr *igError
	if errors.As(err, &igErr) {
		return igErr.Msg
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Instagram request took too long. Please try again."
	}
	return fmt.Sprintf("Failed to fetch Instagram post: %v", err)
}

func ParseInstagramURL(text string) (*ParsedMediaURL, error) {
	if !strings.Contains(text, "instagram.com") && !strings.Contains(text, "ddinstagram.com") {
		return nil, fmt.Errorf("no Instagram URL found")
	}
	if m := igShareRegex.FindStringSubmatch(text); m != nil {
		return &ParsedMediaURL{Source: "instagram", OriginalURL: m[0]}, nil
	}
	if m := igStoryRegex.FindStringSubmatch(text); m != nil {
		return &ParsedMediaURL{Source: "instagram", OriginalURL: m[0]}, nil
	}
	if m := igPostRegex.FindStringSubmatch(text); m != nil {
		return &ParsedMediaURL{Source: "instagram", OriginalURL: m[0]}, nil
	}
	return nil, fmt.Errorf("no Instagram URL found")
}

func FetchInstagramMedia(ctx context.Context, url string) (*MediaResult, error) {
	kind, shortcode, storyUser, storyID := classifyIGURL(url)
	switch kind {
	case "share":
		resolved, err := resolveIGShareLink(ctx, url)
		if err != nil {
			return nil, err
		}
		k, sc, _, _ := classifyIGURL(resolved)
		if k != "post" {
			return nil, &igError{Code: 0, Msg: "Could not resolve Instagram share link to a post."}
		}
		shortcode = sc
	case "story":
		return fetchIGStory(ctx, storyUser, storyID)
	case "post":
	default:
		return nil, fmt.Errorf("unsupported Instagram URL")
	}

	hasCookie := igCookies != nil && igCookies.HasCookie()

	// Stage 1: get media_id (anon first, cookie if anon fails).
	mediaID, err := fetchIGMediaID(ctx, shortcode, "")
	if err != nil || mediaID == "" {
		if hasCookie {
			log.Printf("Instagram oembed anonymous failed for %s (%v); retrying with cookie", shortcode, err)
			id2, err2 := fetchIGMediaID(ctx, shortcode, igCookies.CookieHeader())
			if err2 == nil {
				mediaID = id2
			} else if err == nil {
				err = err2
			}
		}
	}

	// Stage 2: mobile media/info API (anon first, cookie if anon fails).
	if mediaID != "" {
		if result, perr := fetchIGMobileAPI(ctx, mediaID, ""); perr == nil {
			return result, nil
		} else {
			log.Printf("Instagram mobile API anon failed for %s (%v)", shortcode, perr)
		}
		if hasCookie {
			result, perr := fetchIGMobileAPI(ctx, mediaID, igCookies.CookieHeader())
			if perr == nil {
				return result, nil
			}
			log.Printf("Instagram mobile API cookie failed for %s (%v)", shortcode, perr)
			if isIGAuthErr(perr) {
				return nil, &igError{Code: 403, Msg: "🔒 Instagram session cookie may be expired — please refresh INSTAGRAM_COOKIES_FILE."}
			}
		}
	} else if err != nil && isIGAuthErr(err) && !hasCookie {
		// oembed itself demanded auth and we have no cookie to retry with.
		return nil, err
	}

	// Stage 3: HTML embed fallback (anon, then cookie).
	if result, err := fetchIGEmbed(ctx, shortcode, ""); err == nil && result != nil {
		return result, nil
	} else if err != nil {
		log.Printf("Instagram embed anon failed for %s: %v", shortcode, err)
	}
	if hasCookie {
		if result, err := fetchIGEmbed(ctx, shortcode, igCookies.CookieHeader()); err == nil && result != nil {
			return result, nil
		} else if err != nil {
			log.Printf("Instagram embed cookie failed for %s: %v", shortcode, err)
		}
	}

	// Stage 4: Web GraphQL fallback (anon, then cookie).
	if result, err := fetchIGGraphQL(ctx, shortcode, ""); err == nil && result != nil {
		return result, nil
	} else if err != nil {
		log.Printf("Instagram GraphQL anon failed for %s: %v", shortcode, err)
	}
	if hasCookie {
		if result, err := fetchIGGraphQL(ctx, shortcode, igCookies.CookieHeader()); err == nil && result != nil {
			return result, nil
		} else if err != nil {
			log.Printf("Instagram GraphQL cookie failed for %s: %v", shortcode, err)
		}
	}

	return nil, &igError{Code: 0, Msg: "Could not fetch Instagram post. The post may be private, deleted, or require login."}
}

func isIGAuthErr(err error) bool {
	var igErr *igError
	if errors.As(err, &igErr) {
		return igErr.Code == 401 || igErr.Code == 403
	}
	return false
}

func classifyIGURL(url string) (kind, shortcode, storyUser, storyID string) {
	if m := igShareRegex.FindStringSubmatch(url); m != nil {
		return "share", m[1], "", ""
	}
	if m := igStoryRegex.FindStringSubmatch(url); m != nil {
		return "story", "", m[1], m[2]
	}
	if m := igPostRegex.FindStringSubmatch(url); m != nil {
		return "post", m[2], "", ""
	}
	return "", "", "", ""
}

func resolveIGShareLink(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			req.Header.Set("User-Agent", "curl/7.88.1")
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/7.88.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.Request != nil && resp.Request.URL != nil {
		final := resp.Request.URL.String()
		if igCanonicalRegex.MatchString(final) {
			return final, nil
		}
	}
	if cm := igCanonicalRegex.Find(body); cm != nil {
		return string(cm), nil
	}
	return "", &igError{Code: 0, Msg: "Could not resolve Instagram share link."}
}

func fetchIGStory(ctx context.Context, storyUser, storyID string) (*MediaResult, error) {
	// Try with cookie if we have one; stories almost always require auth.
	cookie := ""
	if igCookies != nil && igCookies.HasCookie() {
		cookie = igCookies.CookieHeader()
	}
	result, err := fetchIGMobileAPI(ctx, storyID, cookie)
	if err == nil {
		return result, nil
	}
	var igErr *igError
	if errors.As(err, &igErr) {
		return nil, &igError{Code: igErr.Code, Msg: "Instagram stories require login to view."}
	}
	return nil, fmt.Errorf("Instagram stories require login to view: %v", err)
}

func fetchIGMediaID(ctx context.Context, shortcode, cookie string) (string, error) {
	url := fmt.Sprintf("https://i.instagram.com/api/v1/oembed/?url=https://www.instagram.com/p/%s/", shortcode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	for k, vs := range igMobileHeaders(cookie) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := igHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	igUpdateJarFromResponse(resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err := igStatusErr(resp.StatusCode); err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("oembed API: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var oembed struct {
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(body, &oembed); err != nil {
		return "", fmt.Errorf("parse oembed: %w", err)
	}
	if oembed.MediaID == "" {
		return "", fmt.Errorf("oembed returned empty media_id")
	}
	return oembed.MediaID, nil
}

func fetchIGMobileAPI(ctx context.Context, mediaID, cookie string) (*MediaResult, error) {
	url := fmt.Sprintf("https://i.instagram.com/api/v1/media/%s/info/", mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range igMobileHeaders(cookie) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := igHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	igUpdateJarFromResponse(resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err := igStatusErr(resp.StatusCode); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("mobile API: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return parseIGMobileResponse(body)
}

func igStatusErr(code int) error {
	switch code {
	case 401, 403:
		return &igError{Code: code, Msg: "🔒 Instagram requires login to view this content."}
	case 404:
		return &igError{Code: 404, Msg: "Instagram post not found."}
	case 429:
		return &igError{Code: 429, Msg: "⏱️ Instagram rate limit exceeded. Try again later."}
	}
	return nil
}

type igVideoCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type igImageVersions struct {
	Candidates []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"candidates"`
}

func parseIGMobileResponse(body []byte) (*MediaResult, error) {
	var resp struct {
		Items []struct {
			CarouselMedia []struct {
				VideoVersions  []igVideoCandidate `json:"video_versions"`
				ImageVersions2 *igImageVersions   `json:"image_versions2"`
			} `json:"carousel_media"`
			VideoVersions  []igVideoCandidate `json:"video_versions"`
			ImageVersions2 *igImageVersions   `json:"image_versions2"`
			Caption        struct {
				Text string `json:"text"`
			} `json:"caption"`
			User struct {
				Username string `json:"username"`
				FullName string `json:"full_name"`
			} `json:"user"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse media info: %w", err)
	}
	if len(resp.Items) == 0 {
		return nil, fmt.Errorf("no items in mobile API response")
	}
	item := resp.Items[0]
	result := &MediaResult{}
	if item.Caption.Text != "" {
		result.Text = html.EscapeString(item.Caption.Text)
		result.TextIsHTML = true
	}
	if item.User.Username != "" {
		result.Author = item.User.FullName
		if result.Author == "" {
			result.Author = "@" + item.User.Username
		}
		result.AuthorURL = fmt.Sprintf("https://www.instagram.com/%s/", item.User.Username)
	}

	var images []MediaImage
	var bestVideoURL string
	bestVideoArea := 0
	hasVideo := false

	if len(item.CarouselMedia) > 0 {
		for _, ci := range item.CarouselMedia {
			if len(ci.VideoVersions) > 0 {
				hasVideo = true
				best := pickBestIGVideo(ci.VideoVersions)
				if a := best.Width * best.Height; a > bestVideoArea {
					bestVideoArea = a
					bestVideoURL = best.URL
				}
			}
			if ci.ImageVersions2 != nil && len(ci.ImageVersions2.Candidates) > 0 {
				images = append(images, MediaImage{
					Fullsize: ci.ImageVersions2.Candidates[0].URL,
					Thumb:    ci.ImageVersions2.Candidates[0].URL,
				})
			}
		}
	} else if len(item.VideoVersions) > 0 {
		hasVideo = true
		best := pickBestIGVideo(item.VideoVersions)
		bestVideoURL = best.URL
	} else if item.ImageVersions2 != nil && len(item.ImageVersions2.Candidates) > 0 {
		images = []MediaImage{{
			Fullsize: item.ImageVersions2.Candidates[0].URL,
			Thumb:    item.ImageVersions2.Candidates[0].URL,
		}}
	}

	if hasVideo && bestVideoURL != "" {
		result.Video = &MediaVideo{
			DirectURL: bestVideoURL,
			Variants:  []VideoVariant{{URL: bestVideoURL}},
		}
	}
	if !hasVideo && len(images) > 0 {
		result.Images = images
	}
	return result, nil
}

func pickBestIGVideo(vs []igVideoCandidate) igVideoCandidate {
	if len(vs) == 0 {
		return igVideoCandidate{}
	}
	best := vs[0]
	bestArea := best.Width * best.Height
	for _, v := range vs[1:] {
		if a := v.Width * v.Height; a > bestArea {
			bestArea = a
			best = v
		}
	}
	return best
}

func fetchIGEmbed(ctx context.Context, shortcode, cookie string) (*MediaResult, error) {
	url := fmt.Sprintf("https://www.instagram.com/p/%s/embed/captioned/", shortcode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range igEmbedHeaders(cookie) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := igHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	igUpdateJarFromResponse(resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err := igStatusErr(resp.StatusCode); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embed page: HTTP %d", resp.StatusCode)
	}
	data := string(body)
	result := &MediaResult{}

	// Try the richer contextJSON parse first; fall back to og:meta.
	if res, ok := parseIGEmbedContextJSON([]byte(data)); ok && res != nil {
		return res, nil
	}

	var ogVideo string
	if m := ogVideoRegex.FindStringSubmatch(data); m != nil {
		ogVideo = m[1]
	}
	var ogImage string
	if m := ogImageRegex.FindStringSubmatch(data); m != nil {
		ogImage = m[1]
	}
	if ogVideo != "" {
		result.Video = &MediaVideo{
			DirectURL:    ogVideo,
			ThumbnailURL: ogImage,
			Variants:     []VideoVariant{{URL: ogVideo}},
		}
	} else if ogImage != "" {
		result.Images = []MediaImage{{
			Fullsize: ogImage,
			Thumb:    ogImage,
		}}
	} else {
		return nil, fmt.Errorf("no media found in Instagram embed page")
	}
	if m := ogDescriptionRegex.FindStringSubmatch(data); m != nil {
		result.Text = html.EscapeString(igCleanCaption(m[1]))
		result.TextIsHTML = true
	}
	return result, nil
}

// parseIGEmbedContextJSON parses the embed page's inline `"init",[],[...]`
// JSON blob. Returns (result, true) on success.
func parseIGEmbedContextJSON(body []byte) (*MediaResult, bool) {
	m := igEmbedInitRegex.FindSubmatch(body)
	if m == nil {
		return nil, false
	}
	var outer []json.RawMessage
	if err := json.Unmarshal(m[1], &outer); err != nil || len(outer) == 0 {
		return nil, false
	}
	// Find the object containing contextJSON.
	for _, raw := range outer {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		ctxRaw, ok := probe["contextJSON"]
		if !ok {
			continue
		}
		var ctx struct {
			MediaContext struct {
				MediaID  string `json:"media_id"`
				Image    string `json:"image"`
				Video    string `json:"video"`
				Thumbnail string `json:"thumbnail"`
				Caption  string `json:"caption"`
				Owner    struct {
					Username string `json:"username"`
				} `json:"owner"`
				Sidecar  []json.RawMessage `json:"sidecar"`
			} `json:"mediaContext"`
        }
		if err := json.Unmarshal(ctxRaw, &ctx); err != nil {
			continue
		}
		result := &MediaResult{}
		mc := ctx.MediaContext
		if mc.Video != "" {
			result.Video = &MediaVideo{
				DirectURL:    mc.Video,
				ThumbnailURL: mc.Thumbnail,
				Variants:     []VideoVariant{{URL: mc.Video}},
			}
		} else if len(mc.Sidecar) > 0 {
			// Image carousel
			var imgs []MediaImage
			for _, sraw := range mc.Sidecar {
				var s struct {
					DisplayURL string `json:"display_url"`
				}
				if json.Unmarshal(sraw, &s) == nil && s.DisplayURL != "" {
					imgs = append(imgs, MediaImage{Fullsize: s.DisplayURL, Thumb: s.DisplayURL})
				}
			}
			if len(imgs) > 0 {
				result.Images = imgs
			}
		} else if mc.Image != "" {
			result.Images = []MediaImage{{Fullsize: mc.Image, Thumb: mc.Image}}
		} else {
			continue
		}
		if mc.Caption != "" {
			result.Text = html.EscapeString(mc.Caption)
			result.TextIsHTML = true
		}
		if mc.Owner.Username != "" {
			result.Author = mc.Owner.Username
			result.AuthorURL = fmt.Sprintf("https://www.instagram.com/%s/", mc.Owner.Username)
		}
		return result, true
	}
	return nil, false
}

func igCleanCaption(s string) string {
	const marker = ` on Instagram: "`
	if i := strings.Index(s, marker); i >= 0 {
		rest := s[i+len(marker):]
		if j := strings.LastIndex(rest, `"`); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	if i := strings.Index(s, " - "); i > 0 {
		return s[:i]
	}
	return s
}

// fetchIGGraphQL fetches the post HTML, scrapes anti-bot tokens (LSD, csrf,
// device IDs), and POSTs to /graphql/query for the post data. This is the
// most reliable logged-out strategy.
func fetchIGGraphQL(ctx context.Context, shortcode, cookie string) (*MediaResult, error) {
	pageURL := fmt.Sprintf("https://www.instagram.com/p/%s/", shortcode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range igEmbedHeaders(cookie) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := igHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	igUpdateJarFromResponse(resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("graphql page fetch: HTTP %d", resp.StatusCode)
	}

	params, err := igExtractGQLParams(body, cookie)
	if err != nil {
		return nil, err
	}

	gqlURL := "https://www.instagram.com/graphql/query"
	form := url.Values{}
	for k, v := range params.body {
		form.Set(k, v)
	}
	form.Set("fb_api_caller_class", "RelayModern")
	form.Set("fb_api_req_friendly_name", "PolarisPostActionLoadPostQueryQuery")
	form.Set("server_timestamps", "true")
	form.Set("doc_id", igGraphQLDocID)
	variables, _ := json.Marshal(map[string]interface{}{
		"shortcode":                shortcode,
		"fetch_tagged_user_count":  nil,
		"hoisted_comment_id":       nil,
		"hoisted_reply_id":         nil,
	})
	form.Set("variables", string(variables))

	greq, err := http.NewRequestWithContext(ctx, http.MethodPost, gqlURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	for k, vs := range igEmbedHeaders(cookie) {
		for _, v := range vs {
			greq.Header.Set(k, v)
		}
	}
	greq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range params.headers {
		greq.Header.Set(k, v)
	}

	gresp, err := igHTTPClient.Do(greq)
	if err != nil {
		return nil, err
	}
	defer gresp.Body.Close()
	igUpdateJarFromResponse(gresp)
	gqlBody, _ := io.ReadAll(io.LimitReader(gresp.Body, 1024*1024))
	if gresp.StatusCode != 200 {
		return nil, fmt.Errorf("graphql query: HTTP %d: %s", gresp.StatusCode, string(gqlBody))
	}

	return parseIGGraphQLResponse(gqlBody)
}

// igGQLParams holds the dynamic tokens scraped from the post page needed
// to POST to /graphql/query.
type igGQLParams struct {
	headers map[string]string
	body    map[string]string
}

// igExtractGQLParams scrapes SiteData, PolaristSiteData, LSD, csrf, etc.
func igExtractGQLParams(body []byte, existingCookie string) (igGQLParams, error) {
	htmlStr := string(body)
	getObj := func(re *regexp.Regexp) map[string]json.RawMessage {
		m := re.FindSubmatch(body)
		if m == nil {
			return nil
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(m[1], &obj) != nil {
			return nil
		}
		return obj
	}
	siteData := getObj(igRESiteData)
	polaris := getObj(igREPolarisSiteData)
	dgw := getObj(igREDGWWebConfig)
	push := getObj(igREPushInfo)
	lsdObj := getObj(igRELSD)
	sec := getObj(igRESecurityConfig)
	bloks := getObj(igREBloksVersioningID)

	var lsdToken string
	if lsdObj != nil {
		var s string
		if json.Unmarshal(lsdObj["token"], &s) == nil && s != "" {
			lsdToken = s
		}
	}
	if lsdToken == "" {
		lsdToken = randBase64URL(8)
	}

	var csrf string
	if sec != nil {
		var s string
		if json.Unmarshal(sec["csrf_token"], &s) == nil {
			csrf = s
		}
	}

	var appID string
	if dgw != nil {
		var s string
		if json.Unmarshal(dgw["appId"], &s) == nil && s != "" {
			appID = s
		}
	}
	if appID == "" {
		appID = igAppID
	}

	var deviceID, machineID string
	if polaris != nil {
		var s string
		if json.Unmarshal(polaris["device_id"], &s) == nil {
			deviceID = s
		}
		if json.Unmarshal(polaris["machine_id"], &s) == nil {
			machineID = s
		}
	}

	var hasteSession, hsi, spinR, spinB, spinT string
	if siteData != nil {
		var s string
		if json.Unmarshal(siteData["haste_session"], &s) == nil && s != "" {
			hasteSession = s
		}
		if json.Unmarshal(siteData["hsi"], &s) == nil && s != "" {
			hsi = s
		}
		if json.Unmarshal(siteData["__spin_r"], &s) == nil && s != "" {
			spinR = s
		}
		if json.Unmarshal(siteData["__spin_b"], &s) == nil && s != "" {
			spinB = s
		}
		if t, err := parseJSONNumber(siteData["__spin_t"]); err == nil && t > 0 {
			spinT = fmt.Sprintf("%d", int64(t))
		}
	}
	if hasteSession == "" {
		hasteSession = "20126.HYP:instagram_web_pkg.2.1...0"
	}
	if hsi == "" {
		hsi = "7436540909012459023"
	}

	var rolloutHash string
	if push != nil {
		var s string
		if json.Unmarshal(push["rollout_hash"], &s) == nil && s != "" {
			rolloutHash = s
		}
	}
	if rolloutHash == "" {
		rolloutHash = "1019933358"
	}

	var cometReq string
	if m := igRECometReq.FindStringSubmatch(htmlStr); m != nil && m[1] != "" {
		cometReq = m[1]
	}
	if cometReq == "" {
		cometReq = "7"
	}

	var jazoest string
	if m := igREJazoest.FindStringSubmatch(htmlStr); m != nil && m[1] != "" {
		jazoest = m[1]
	}
	if jazoest == "" {
		jazoest = fmt.Sprintf("%d", irandInt(10000))
	}

	var bloksVer string
	if bloks != nil {
		var s string
		if json.Unmarshal(bloks["versioningID"], &s) == nil && s != "" {
			bloksVer = s
		}
	}

	if spinR == "" {
		spinR = rolloutHash
	}
	if spinB == "" {
		spinB = "trunk"
	}
	if spinT == "" {
		spinT = fmt.Sprintf("%d", time.Now().Unix())
	}

	// Build anonymous cookie (merged with existing if provided).
	cookieParts := []string{}
	if existingCookie != "" {
		cookieParts = append(cookieParts, existingCookie)
	}
	if csrf != "" && !strings.Contains(existingCookie, "csrftoken=") {
		cookieParts = append(cookieParts, "csrftoken="+csrf)
	}
	if deviceID != "" && !strings.Contains(existingCookie, "ig_did=") {
		cookieParts = append(cookieParts, "ig_did="+deviceID)
	}
	cookieParts = append(cookieParts, "wd=1280x720", "dpr=2")
	if machineID != "" && !strings.Contains(existingCookie, "mid=") {
		cookieParts = append(cookieParts, "mid="+machineID)
	}
	cookieParts = append(cookieParts, "ig_nrcb=1")
	anonCookie := strings.Join(cookieParts, "; ")

	headers := map[string]string{
		"x-ig-app-id":           appID,
		"X-FB-LSD":              lsdToken,
		"X-CSRFToken":           csrf,
		"X-Bloks-Version-Id":    bloksVer,
		"x-asbd-id":             "129477",
		"X-FB-Friendly-Name":    "PolarisPostActionLoadPostQueryQuery",
		"Cookie":                anonCookie,
	}

	b := map[string]string{
		"__d":         "www",
		"__a":         "1",
		"__s":         "::" + randString(6),
		"__hs":        hasteSession,
		"__req":       "b",
		"__ccg":       "EXCELLENT",
		"__rev":       rolloutHash,
		"__hsi":       hsi,
		"__dyn":       randBase64URL(154),
		"__csr":       randBase64URL(154),
		"__user":      "0",
		"__comet_req": cometReq,
		"av":          "0",
		"dpr":         "2",
		"lsd":         lsdToken,
		"jazoest":     jazoest,
		"__spin_r":    spinR,
		"__spin_b":    spinB,
		"__spin_t":    spinT,
	}

	return igGQLParams{headers: headers, body: b}, nil
}

// parseIGGraphQLResponse extracts the post media from a /graphql/query
// response.
func parseIGGraphQLResponse(body []byte) (*MediaResult, error) {
	var resp struct {
		Data struct {
			ShortcodeMedia    *igGQLShortcodeMedia `json:"shortcode_media"`
			XDTShortcodeMedia *igGQLShortcodeMedia `json:"xdt_shortcode_media"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse graphql: %w", err)
	}
	sm := resp.Data.ShortcodeMedia
	if sm == nil {
		sm = resp.Data.XDTShortcodeMedia
	}
	if sm == nil {
		return nil, fmt.Errorf("graphql response has no shortcode_media")
	}
	result := &MediaResult{}

	var images []MediaImage
	var bestVideoURL string
	hasVideo := false

	if len(sm.EdgeSidecarToChildren.Edges) > 0 {
		for _, e := range sm.EdgeSidecarToChildren.Edges {
			node := e.Node
			if node.IsVideo && node.VideoURL != "" {
				hasVideo = true
				// GraphQL gives a single video_url per sidecar item; no
				// explicit width/height, so we can't pick "largest"; we
				// take the first video.
				if bestVideoURL == "" {
					bestVideoURL = node.VideoURL
				}
			} else if node.DisplayURL != "" {
				images = append(images, MediaImage{Fullsize: node.DisplayURL, Thumb: node.DisplayURL})
			}
		}
	} else if sm.VideoURL != "" {
		hasVideo = true
		bestVideoURL = sm.VideoURL
	} else if sm.DisplayURL != "" {
		images = []MediaImage{{Fullsize: sm.DisplayURL, Thumb: sm.DisplayURL}}
	}

	if hasVideo && bestVideoURL != "" {
		result.Video = &MediaVideo{
			DirectURL:   bestVideoURL,
			Variants:    []VideoVariant{{URL: bestVideoURL}},
			ThumbnailURL: sm.DisplayURL,
		}
	}
	if !hasVideo && len(images) > 0 {
		result.Images = images
	}
	if result.Video == nil && result.Images == nil {
		return nil, fmt.Errorf("graphql: no media found in shortcode_media")
	}
	if sm.Owner.Username != "" {
		result.Author = sm.Owner.Username
		result.AuthorURL = fmt.Sprintf("https://www.instagram.com/%s/", sm.Owner.Username)
	}
	if sm.EdgeMediaToCaption.Edges != nil && len(sm.EdgeMediaToCaption.Edges) > 0 {
		caption := sm.EdgeMediaToCaption.Edges[0].Node.Text
		if caption != "" {
			result.Text = html.EscapeString(caption)
			result.TextIsHTML = true
		}
	}
	return result, nil
}

// igGQLShortcodeMedia mirrors the GraphQL shortcode_media shape.
type igGQLShortcodeMedia struct {
	VideoURL    string `json:"video_url"`
	DisplayURL  string `json:"display_url"`
	IsVideo     bool   `json:"is_video"`
	Owner       struct {
		Username string `json:"username"`
	} `json:"owner"`
	EdgeSidecarToChildren struct {
		Edges []struct {
			Node struct {
				DisplayURL string `json:"display_url"`
				IsVideo    bool   `json:"is_video"`
				VideoURL   string `json:"video_url"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"edge_sidecar_to_children"`
	EdgeMediaToCaption struct {
		Edges []struct {
			Node struct {
				Text string `json:"text"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"edge_media_to_caption"`
}