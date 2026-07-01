package main

import "testing"

func TestParseTwitterURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  string
		wantErr bool
	}{
		{"x.com", "https://x.com/multikigu/status/2071655663516668318", "2071655663516668318", false},
		{"twitter.com", "https://twitter.com/multikigu/status/2071655663516668318", "2071655663516668318", false},
		{"fxtwitter.com", "https://fxtwitter.com/multikigu/status/2071655663516668318", "2071655663516668318", false},
		{"vxtwitter.com", "https://vxtwitter.com/multikigu/status/2071655663516668318", "2071655663516668318", false},
		{"fixupx.com", "https://fixupx.com/multikigu/status/2071655663516668318", "2071655663516668318", false},
		{"with video suffix", "https://x.com/multikigu/status/2071655663516668318/video/1", "2071655663516668318", false},
		{"with photo suffix", "https://x.com/Rony39948830/status/2072041604622303428/photo/1", "2072041604622303428", false},
		{"http", "http://x.com/user/status/123", "123", false},
		{"invalid", "https://bsky.app/profile/user/post/abc", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTwitterURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTwitterURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Rkey != tt.wantID {
				t.Errorf("Rkey = %q, want %q", got.Rkey, tt.wantID)
			}
			if got.Source != "twitter" {
				t.Errorf("Source = %q, want twitter", got.Source)
			}
		})
	}
}

func TestParseTweetResponse(t *testing.T) {
	data := []byte(`{"status":{"id":"2072041604622303428","text":"My first handmade figure","url":"https://x.com/Rony39948830/status/2072041604622303428","media":{"photos":[{"url":"https://pbs.twimg.com/media/test.jpg","width":1779,"height":2048}],"videos":[]},"author":{"name":"RonyDLnerd","screen_name":"Rony39948830","url":"https://x.com/Rony39948830"}},"code":200}`)

	result, err := parseTweetResponse(data)
	if err != nil {
		t.Fatalf("parseTweetResponse: %v", err)
	}
	if result.Author != "RonyDLnerd" {
		t.Errorf("Author = %q, want RonyDLnerd", result.Author)
	}
	if result.AuthorURL != "https://x.com/Rony39948830" {
		t.Errorf("AuthorURL = %q", result.AuthorURL)
	}
	if result.SubmissionURL != "https://x.com/Rony39948830/status/2072041604622303428" {
		t.Errorf("SubmissionURL = %q", result.SubmissionURL)
	}
	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	if result.Images[0].Fullsize != "https://pbs.twimg.com/media/test.jpg" {
		t.Errorf("Image URL = %q", result.Images[0].Fullsize)
	}
	if result.Video != nil {
		t.Errorf("Video should be nil for photo tweet")
	}
}

func TestParseTweetResponseVideo(t *testing.T) {
	data := []byte(`{"status":{"id":"2071655663516668318","text":"video tweet","url":"https://x.com/multikigu/status/2071655663516668318","media":{"photos":[],"videos":[{"url":"https://video.twimg.com/test.mp4","thumbnail_url":"https://pbs.twimg.com/thumb.jpg","width":720,"height":1280}]},"author":{"name":"Multikigu","screen_name":"multikigu","url":"https://x.com/multikigu"}},"code":200}`)

	result, err := parseTweetResponse(data)
	if err != nil {
		t.Fatalf("parseTweetResponse: %v", err)
	}
	if result.Video == nil {
		t.Fatal("Video should not be nil")
	}
	if result.Video.DirectURL != "https://video.twimg.com/test.mp4" {
		t.Errorf("Video URL = %q", result.Video.DirectURL)
	}
	if result.Video.ThumbnailURL != "https://pbs.twimg.com/thumb.jpg" {
		t.Errorf("ThumbnailURL = %q", result.Video.ThumbnailURL)
	}
	if len(result.Images) != 0 {
		t.Errorf("Images should be empty for video tweet")
	}
}
