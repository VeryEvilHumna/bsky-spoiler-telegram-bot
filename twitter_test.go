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
		{"with query params", "https://x.com/user/status/123?s=20", "123", false},
		{"with query params and text", "https://x.com/user/status/123?s=20 extra text", "123", false},
		{"with fragment", "https://x.com/user/status/123#section", "123", false},
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

func TestParseTwitterURLIncludesQueryParams(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantOrigURL string
	}{
		{"no query", "https://x.com/user/status/123", "https://x.com/user/status/123"},
		{"with query", "https://x.com/user/status/123?s=20", "https://x.com/user/status/123?s=20"},
		{"with fragment", "https://x.com/user/status/123#sec", "https://x.com/user/status/123#sec"},
		{"with query and fragment", "https://x.com/user/status/123?s=20#sec", "https://x.com/user/status/123?s=20#sec"},
		{"with query and trailing text", "https://x.com/user/status/123?s=20 extra", "https://x.com/user/status/123?s=20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTwitterURL(tt.url)
			if err != nil {
				t.Fatalf("ParseTwitterURL(%q) error: %v", tt.url, err)
			}
			if got.OriginalURL != tt.wantOrigURL {
				t.Errorf("OriginalURL = %q, want %q", got.OriginalURL, tt.wantOrigURL)
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

func TestParseTweetResponseMentions(t *testing.T) {
	data := []byte(`{"status":{"id":"2071655663516668318","text":"Mask maker: @mjnln274668 #kigurumi https://t.co/test","url":"https://x.com/multikigu/status/2071655663516668318","raw_text":{"text":"Mask maker: @mjnln274668 #kigurumi https://t.co/test","facets":[{"type":"mention","indices":[12,24],"original":"mjnln274668"},{"type":"hashtag","indices":[25,34],"original":"kigurumi"},{"type":"media","indices":[35,52],"id":"123","display":"pic.x.com/test","original":"https://t.co/test","replacement":"https://x.com/multikigu/status/2071655663516668318/video/1"}]},"media":{"photos":[],"videos":[{"url":"https://video.twimg.com/test.mp4","thumbnail_url":"https://pbs.twimg.com/thumb.jpg","width":720,"height":1280}]},"author":{"name":"Multikigu","screen_name":"multikigu","url":"https://x.com/multikigu"}},"code":200}`)

	result, err := parseTweetResponse(data)
	if err != nil {
		t.Fatalf("parseTweetResponse: %v", err)
	}
	expected := `Mask maker: <b><a href="https://x.com/mjnln274668">@mjnln274668</a></b> #kigurumi `
	if result.Text != expected {
		t.Errorf("Text = %q, want %q", result.Text, expected)
	}
}

func TestFormatTweetText(t *testing.T) {
	raw := struct {
		Text   string `json:"text"`
		Facets []struct {
			Type string `json:"type"`
			Indices [2]int `json:"indices"`
			Original string `json:"original"`
		} `json:"facets"`
	}{
		Text: "Hello @user1 and @user2! <script> #tag https://t.co/abc",
		Facets: []struct {
			Type string `json:"type"`
			Indices [2]int `json:"indices"`
			Original string `json:"original"`
		}{
			{Type: "mention", Indices: [2]int{6, 12}, Original: "user1"},
			{Type: "mention", Indices: [2]int{17, 23}, Original: "user2"},
			{Type: "hashtag", Indices: [2]int{34, 38}, Original: "tag"},
			{Type: "media", Indices: [2]int{39, 56}, Original: "https://t.co/abc"},
		},
	}
	result := formatTweetText(raw)
	expected := `Hello <b><a href="https://x.com/user1">@user1</a></b> and <b><a href="https://x.com/user2">@user2</a></b>! &lt;script&gt; #tag `
	if result != expected {
		t.Errorf("formatTweetText = %q, want %q", result, expected)
	}
}
