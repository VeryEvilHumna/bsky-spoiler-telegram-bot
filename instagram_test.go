package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInstagramURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKind  string
		wantShort string
		wantUser  string
		wantStory string
		wantErr   bool
	}{
		{"post bare", "https://www.instagram.com/p/Cabc123/", "post", "Cabc123", "", "", false},
		{"post no www", "https://instagram.com/p/Cabc123/", "post", "Cabc123", "", "", false},
		{"reel", "https://www.instagram.com/reel/Cabc123/", "post", "Cabc123", "", "", false},
		{"reels", "https://www.instagram.com/reels/Cabc123/", "post", "Cabc123", "", "", false},
		{"tv", "https://www.instagram.com/tv/Cabc123/", "post", "Cabc123", "", "", false},
		{"user post", "https://www.instagram.com/some_user/p/Cabc123/", "post", "Cabc123", "", "", false},
		{"user reel", "https://www.instagram.com/some.user/reel/Cabc123/", "post", "Cabc123", "", "", false},
		{"ddinstagram post", "https://ddinstagram.com/p/Cabc123/", "post", "Cabc123", "", "", false},
		{"ddinstagram reel", "https://ddinstagram.com/reel/Cabc123/", "post", "Cabc123", "", "", false},
		{"story", "https://www.instagram.com/stories/some_user/1234567890", "story", "", "some_user", "1234567890", false},
		{"share bare", "https://www.instagram.com/share/AbC_123", "share", "AbC_123", "", "", false},
		{"share p", "https://www.instagram.com/share/p/Cabc123", "share", "Cabc123", "", "", false},
		{"share reel", "https://www.instagram.com/share/reel/Cabc123", "share", "Cabc123", "", "", false},
		{"embedded in text", "look at this https://www.instagram.com/p/Cabc123/ cool", "post", "Cabc123", "", "", false},
		{"invalid domain", "https://example.com/p/Cabc123", "", "", "", "", true},
		{"empty", "", "", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseInstagramURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseInstagramURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if parsed.Source != "instagram" {
				t.Errorf("Source = %q, want instagram", parsed.Source)
			}
			kind, shortcode, storyUser, storyID := classifyIGURL(parsed.OriginalURL)
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q (orig=%q)", kind, tt.wantKind, parsed.OriginalURL)
			}
			if shortcode != tt.wantShort {
				t.Errorf("shortcode = %q, want %q (orig=%q)", shortcode, tt.wantShort, parsed.OriginalURL)
			}
			if storyUser != tt.wantUser {
				t.Errorf("storyUser = %q, want %q", storyUser, tt.wantUser)
			}
			if storyID != tt.wantStory {
				t.Errorf("storyID = %q, want %q", storyID, tt.wantStory)
			}
		})
	}
}

func TestParseIGMobileResponseSingleImage(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":"look at this!"},"user":{"username":"alice","full_name":"Alice Doe"},"image_versions2":{"candidates":[{"url":"https://scontent.cdninstagram.com/img.jpg","width":1080,"height":1080}]}}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if result.Video != nil {
		t.Errorf("Video should be nil")
	}
	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	if result.Images[0].Fullsize != "https://scontent.cdninstagram.com/img.jpg" {
		t.Errorf("Image URL = %q", result.Images[0].Fullsize)
	}
	if result.Author != "Alice Doe" {
		t.Errorf("Author = %q, want Alice Doe", result.Author)
	}
	if result.AuthorURL != "https://www.instagram.com/alice/" {
		t.Errorf("AuthorURL = %q", result.AuthorURL)
	}
}

func TestParseIGMobileResponseSingleVideo(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":""},"user":{"username":"bob","full_name":""},"video_versions":[{"url":"https://scontent.cdninstagram.com/low.mp4","width":480,"height":854},{"url":"https://scontent.cdninstagram.com/high.mp4","width":1080,"height":1920}],"image_versions2":{"candidates":[{"url":"https://scontent.cdninstagram.com/thumb.jpg","width":360,"height":640}]}}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if result.Video == nil {
		t.Fatal("Video should not be nil")
	}
	if result.Video.DirectURL != "https://scontent.cdninstagram.com/high.mp4" {
		t.Errorf("Video URL = %q, want highest resolution", result.Video.DirectURL)
	}
	if len(result.Images) != 0 {
		t.Errorf("Images should be empty for video post")
	}
	if result.Author != "@bob" {
		t.Errorf("Author = %q, want @bob (fallback when full_name empty)", result.Author)
	}
}

func TestParseIGMobileResponseCarouselAllImages(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":"carousel"},"user":{"username":"alice","full_name":"Alice"},"carousel_media":[
		{"image_versions2":{"candidates":[{"url":"https://cdn/img1.jpg","width":1080,"height":1080}]}},
		{"image_versions2":{"candidates":[{"url":"https://cdn/img2.jpg","width":1080,"height":1080}]}},
		{"image_versions2":{"candidates":[{"url":"https://cdn/img3.jpg","width":1080,"height":1080}]}}
	]}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if result.Video != nil {
		t.Errorf("Video should be nil for image carousel")
	}
	if len(result.Images) != 3 {
		t.Fatalf("Images len = %d, want 3", len(result.Images))
	}
	if result.Images[0].Fullsize != "https://cdn/img1.jpg" {
		t.Errorf("Images[0] = %q", result.Images[0].Fullsize)
	}
}

func TestParseIGMobileResponseCarouselMixedPrefersVideo(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":"mixed"},"user":{"username":"alice","full_name":"Alice"},"carousel_media":[
		{"image_versions2":{"candidates":[{"url":"https://cdn/img1.jpg","width":1080,"height":1080}]}},
		{"video_versions":[{"url":"https://cdn/v.mp4","width":720,"height":1280}]},
		{"image_versions2":{"candidates":[{"url":"https://cdn/img2.jpg","width":1080,"height":1080}]}}
	]}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if result.Video == nil {
		t.Fatal("Video should not be nil for mixed carousel (prefer video)")
	}
	if result.Video.DirectURL != "https://cdn/v.mp4" {
		t.Errorf("Video URL = %q", result.Video.DirectURL)
	}
	if len(result.Images) != 0 {
		t.Errorf("Images should be empty when carousel has video (got %d)", len(result.Images))
	}
}

func TestParseIGMobileResponseCarouselPicksLargestVideo(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":"multi-video"},"user":{"username":"alice","full_name":"Alice"},"carousel_media":[
		{"video_versions":[{"url":"https://cdn/small.mp4","width":480,"height":854}]},
		{"video_versions":[{"url":"https://cdn/big.mp4","width":1080,"height":1920}]}
	]}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if result.Video == nil {
		t.Fatal("Video should not be nil")
	}
	if result.Video.DirectURL != "https://cdn/big.mp4" {
		t.Errorf("Video URL = %q, want biggest by area", result.Video.DirectURL)
	}
}

func TestParseIGMobileResponseEmpty(t *testing.T) {
	data := []byte(`{"items":[]}`)
	_, err := parseIGMobileResponse(data)
	if err == nil {
		t.Fatal("expected error for empty items")
	}
}

func TestParseIGMobileResponseEscapesCaption(t *testing.T) {
	data := []byte(`{"items":[{"caption":{"text":"<script>alert('x')</script> & <b>bold</b>"},"user":{"username":"a","full_name":"A"},"image_versions2":{"candidates":[{"url":"https://cdn/img.jpg","width":1,"height":1}]}}]}`)
	result, err := parseIGMobileResponse(data)
	if err != nil {
		t.Fatalf("parseIGMobileResponse: %v", err)
	}
	if !result.TextIsHTML {
		t.Errorf("TextIsHTML should be true")
	}
	want := `&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt; &amp; &lt;b&gt;bold&lt;/b&gt;`
	if result.Text != want {
		t.Errorf("Text = %q, want %q", result.Text, want)
	}
}

func TestIGCleanCaption(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"123 Likes, 4 Comments - alice on Instagram: \"caption here\"", "caption here"},
		{"123 Likes, 4 Comments - alice on Instagram: \"caption with \"quotes\" inside\"", `caption with "quotes" inside`},
		{"simple caption", "simple caption"},
		{"no marker text", "no marker text"},
	}
	for _, tt := range tests {
		if got := igCleanCaption(tt.in); got != tt.want {
			t.Errorf("igCleanCaption(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ============================================================
// GraphQL response parser
// ============================================================

func TestParseIGGraphQLResponseImage(t *testing.T) {
	data := []byte(`{"data":{"xdt_shortcode_media":{"display_url":"https://cdn/img.jpg","is_video":false,"owner":{"username":"alice"},"edge_media_to_caption":{"edges":[{"node":{"text":"hi"}}]}}}}`)
	result, err := parseIGGraphQLResponse(data)
	if err != nil {
		t.Fatalf("parseIGGraphQLResponse: %v", err)
	}
	if result.Video != nil {
		t.Errorf("Video should be nil")
	}
	if len(result.Images) != 1 || result.Images[0].Fullsize != "https://cdn/img.jpg" {
		t.Errorf("Images = %#v", result.Images)
	}
	if result.Author != "alice" {
		t.Errorf("Author = %q", result.Author)
	}
	if !result.TextIsHTML || result.Text != "hi" {
		t.Errorf("Text = %q (TextIsHTML=%v)", result.Text, result.TextIsHTML)
	}
}

func TestParseIGGraphQLResponseVideo(t *testing.T) {
	data := []byte(`{"data":{"shortcode_media":{"video_url":"https://cdn/v.mp4","display_url":"https://cdn/thumb.jpg","is_video":true,"owner":{"username":"bob"},"edge_media_to_caption":{"edges":[]}}}}`)
	result, err := parseIGGraphQLResponse(data)
	if err != nil {
		t.Fatalf("parseIGGraphQLResponse: %v", err)
	}
	if result.Video == nil || result.Video.DirectURL != "https://cdn/v.mp4" {
		t.Fatalf("Video = %#v", result.Video)
	}
	if result.Video.ThumbnailURL != "https://cdn/thumb.jpg" {
		t.Errorf("ThumbnailURL = %q", result.Video.ThumbnailURL)
	}
	if len(result.Images) != 0 {
		t.Errorf("Images should be empty")
	}
}

func TestParseIGGraphQLResponseSidecar(t *testing.T) {
	data := []byte(`{"data":{"xdt_shortcode_media":{"display_url":"https://cdn/thumb.jpg","is_video":false,"owner":{"username":"alice"},"edge_sidecar_to_children":{"edges":[
		{"node":{"display_url":"https://cdn/img1.jpg","is_video":false}},
		{"node":{"display_url":"https://cdn/img2.jpg","is_video":false}},
		{"node":{"display_url":"https://cdn/img3.jpg","is_video":false}}
	]},"edge_media_to_caption":{"edges":[]}}}}`)
	result, err := parseIGGraphQLResponse(data)
	if err != nil {
		t.Fatalf("parseIGGraphQLResponse: %v", err)
	}
	if result.Video != nil {
		t.Errorf("Video should be nil for image-only sidecar")
	}
	if len(result.Images) != 3 {
		t.Fatalf("Images len = %d, want 3", len(result.Images))
	}
	if result.Images[0].Fullsize != "https://cdn/img1.jpg" {
		t.Errorf("Images[0] = %q", result.Images[0].Fullsize)
	}
}

func TestParseIGGraphQLResponseMixedCarouselPrefersVideo(t *testing.T) {
	data := []byte(`{"data":{"xdt_shortcode_media":{"display_url":"https://cdn/thumb.jpg","is_video":false,"owner":{"username":"alice"},"edge_sidecar_to_children":{"edges":[
		{"node":{"display_url":"https://cdn/img1.jpg","is_video":false}},
		{"node":{"display_url":"https://cdn/thumb2.jpg","is_video":true,"video_url":"https://cdn/v.mp4"}},
		{"node":{"display_url":"https://cdn/img2.jpg","is_video":false}}
	]},"edge_media_to_caption":{"edges":[]}}}}`)
	result, err := parseIGGraphQLResponse(data)
	if err != nil {
		t.Fatalf("parseIGGraphQLResponse: %v", err)
	}
	if result.Video == nil {
		t.Fatal("Video should not be nil when carousel has a video")
	}
	if result.Video.DirectURL != "https://cdn/v.mp4" {
		t.Errorf("Video URL = %q", result.Video.DirectURL)
	}
	if len(result.Images) != 0 {
		t.Errorf("Images should be empty (got %d)", len(result.Images))
	}
}

func TestParseIGGraphQLResponseEmpty(t *testing.T) {
	data := []byte(`{"data":{}}`)
	if _, err := parseIGGraphQLResponse(data); err == nil {
		t.Fatal("expected error for empty graphql response")
	}
}

// ============================================================
// Cookie jar
// ============================================================

func TestIGCookieJarLoadAndHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ig.json")
	content := `[{"name":"sessionid","value":"ABC","domain":".instagram.com","path":"/","secure":true},{"name":"csrftoken","value":"XYZ","domain":".instagram.com","path":"/","secure":true}]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	jar := loadIGCookieJar(path)
	if !jar.HasCookie() {
		t.Fatal("HasCookie should be true after load")
	}
	h := jar.CookieHeader()
	if !strings.Contains(h, "sessionid=ABC") {
		t.Errorf("CookieHeader missing sessionid: %q", h)
	}
	if !strings.Contains(h, "csrftoken=XYZ") {
		t.Errorf("CookieHeader missing csrftoken: %q", h)
	}
	if jar.CSRFToken() != "XYZ" {
		t.Errorf("CSRFToken = %q, want XYZ", jar.CSRFToken())
	}
}

func TestIGCookieJarEmpty(t *testing.T) {
	jar := loadIGCookieJar("")
	if jar.HasCookie() {
		t.Error("empty jar should not have cookies")
	}
	if h := jar.CookieHeader(); h != "" {
		t.Errorf("CookieHeader = %q, want empty", h)
	}
}

func TestIGCookieJarUpdate(t *testing.T) {
	jar := &igCookieJar{cookies: map[string]string{"sessionid": "OLD"}}
	jar.Update([]string{
		"sessionid=NEW; Path=/; Secure",
		"csrftoken=FRESH; Path=/",
	})
	if jar.cookies["sessionid"] != "NEW" {
		t.Errorf("sessionid = %q, want NEW", jar.cookies["sessionid"])
	}
	if jar.cookies["csrftoken"] != "FRESH" {
		t.Errorf("csrftoken = %q, want FRESH", jar.cookies["csrftoken"])
	}
	// Empty value should remove the cookie.
	jar.Update([]string{"sessionid=; Path=/"})
	if _, ok := jar.cookies["sessionid"]; ok {
		t.Error("sessionid should have been removed")
	}
}

func TestIGCookieJarPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ig.json")
	jar := &igCookieJar{
		cookies:     map[string]string{"sessionid": "ABC"},
		persistPath: path,
	}
	jar.Update([]string{"csrftoken=XYZ"})
	jar.Save()

	loaded := loadIGCookieJar(path)
	if !loaded.HasCookie() {
		t.Fatal("loaded jar should have cookies")
	}
	h := loaded.CookieHeader()
	if !strings.Contains(h, "sessionid=ABC") {
		t.Errorf("persisted CookieHeader missing sessionid: %q", h)
	}
	if !strings.Contains(h, "csrftoken=XYZ") {
		t.Errorf("persisted CookieHeader missing csrftoken: %q", h)
	}
}