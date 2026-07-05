package main

import (
	"encoding/json"
	"testing"

	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

func TestExtractImagesFromImagesView(t *testing.T) {
	post := &bsky.FeedDefs_PostView{
		Embed: &bsky.FeedDefs_PostView_Embed{
			EmbedImages_View: &bsky.EmbedImages_View{
				Images: []*bsky.EmbedImages_ViewImage{
					{Fullsize: "https://cdn.example.com/img1.jpg", Thumb: "https://cdn.example.com/thumb1.jpg", Alt: "alt1"},
					{Fullsize: "https://cdn.example.com/img2.jpg", Thumb: "https://cdn.example.com/thumb2.jpg", Alt: "alt2"},
				},
			},
		},
		Record: &lexutil.LexiconTypeDecoder{},
	}

	imgs := extractImages(post)
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	if imgs[0].Fullsize != "https://cdn.example.com/img1.jpg" {
		t.Errorf("unexpected fullsize: %s", imgs[0].Fullsize)
	}
	if imgs[0].Thumb != "https://cdn.example.com/thumb1.jpg" {
		t.Errorf("unexpected thumb: %s", imgs[0].Thumb)
	}
}

func TestExtractImagesFromGalleryView(t *testing.T) {
	post := &bsky.FeedDefs_PostView{
		Embed: &bsky.FeedDefs_PostView_Embed{
			EmbedGallery_View: &bsky.EmbedGallery_View{
				Items: []*bsky.EmbedGallery_View_Items_Elem{
					{EmbedGallery_ViewImage: &bsky.EmbedGallery_ViewImage{Fullsize: "https://cdn.example.com/gal1.jpg", Thumbnail: "https://cdn.example.com/gal1_thumb.jpg", Alt: "gal1"}},
					{EmbedGallery_ViewImage: &bsky.EmbedGallery_ViewImage{Fullsize: "https://cdn.example.com/gal2.jpg", Thumbnail: "https://cdn.example.com/gal2_thumb.jpg", Alt: "gal2"}},
					{EmbedGallery_ViewImage: &bsky.EmbedGallery_ViewImage{Fullsize: "https://cdn.example.com/gal3.jpg", Thumbnail: "https://cdn.example.com/gal3_thumb.jpg", Alt: "gal3"}},
					{EmbedGallery_ViewImage: &bsky.EmbedGallery_ViewImage{Fullsize: "https://cdn.example.com/gal4.jpg", Thumbnail: "https://cdn.example.com/gal4_thumb.jpg", Alt: "gal4"}},
					{EmbedGallery_ViewImage: &bsky.EmbedGallery_ViewImage{Fullsize: "https://cdn.example.com/gal5.jpg", Thumbnail: "https://cdn.example.com/gal5_thumb.jpg", Alt: "gal5"}},
				},
			},
		},
		Record: &lexutil.LexiconTypeDecoder{},
	}

	imgs := extractImages(post)
	if len(imgs) != 5 {
		t.Fatalf("expected 5 images from gallery, got %d", len(imgs))
	}
	if imgs[0].Fullsize != "https://cdn.example.com/gal1.jpg" {
		t.Errorf("unexpected fullsize: %s", imgs[0].Fullsize)
	}
	if imgs[0].Thumb != "https://cdn.example.com/gal1_thumb.jpg" {
		t.Errorf("unexpected thumb (should come from Thumbnail field): %s", imgs[0].Thumb)
	}
	if imgs[4].Alt != "gal5" {
		t.Errorf("unexpected alt: %s", imgs[4].Alt)
	}
}

func TestExtractImagesFromGalleryViewJSON(t *testing.T) {
	rawJSON := `{
		"$type": "app.bsky.embed.gallery#view",
		"items": [
			{"$type": "app.bsky.embed.gallery#viewImage", "fullsize": "https://cdn.bsky.app/img1.jpg", "thumbnail": "https://cdn.bsky.app/thumb1.jpg", "alt": "", "aspectRatio": {"height": 3088, "width": 2316}},
			{"$type": "app.bsky.embed.gallery#viewImage", "fullsize": "https://cdn.bsky.app/img2.jpg", "thumbnail": "https://cdn.bsky.app/thumb2.jpg", "alt": "", "aspectRatio": {"height": 3088, "width": 2316}},
			{"$type": "app.bsky.embed.gallery#viewImage", "fullsize": "https://cdn.bsky.app/img3.jpg", "thumbnail": "https://cdn.bsky.app/thumb3.jpg", "alt": "", "aspectRatio": {"height": 3088, "width": 2316}},
			{"$type": "app.bsky.embed.gallery#viewImage", "fullsize": "https://cdn.bsky.app/img4.jpg", "thumbnail": "https://cdn.bsky.app/thumb4.jpg", "alt": "", "aspectRatio": {"height": 3088, "width": 2316}},
			{"$type": "app.bsky.embed.gallery#viewImage", "fullsize": "https://cdn.bsky.app/img5.jpg", "thumbnail": "https://cdn.bsky.app/thumb5.jpg", "alt": "", "aspectRatio": {"height": 3088, "width": 2316}}
		]
	}`

	var embed bsky.FeedDefs_PostView_Embed
	if err := json.Unmarshal([]byte(rawJSON), &embed); err != nil {
		t.Fatalf("unmarshal gallery embed: %v", err)
	}

	if embed.EmbedGallery_View == nil {
		t.Fatal("expected EmbedGallery_View to be non-nil")
	}
	if len(embed.EmbedGallery_View.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(embed.EmbedGallery_View.Items))
	}

	post := &bsky.FeedDefs_PostView{
		Embed: &embed,
		Record: &lexutil.LexiconTypeDecoder{},
	}

	imgs := extractImages(post)
	if len(imgs) != 5 {
		t.Fatalf("expected 5 images, got %d", len(imgs))
	}
	for i, img := range imgs {
		if img.Fullsize == "" {
			t.Errorf("image %d: empty Fullsize", i)
		}
		if img.Thumb == "" {
			t.Errorf("image %d: empty Thumb (mapped from Thumbnail)", i)
		}
	}
}
