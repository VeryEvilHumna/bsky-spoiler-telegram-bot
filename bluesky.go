package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

var bskyURLRegex = regexp.MustCompile(`https?://(?:bsky|fxbsky|vxbsky|bskye|bskyx|bsyy)\.app/profile/([a-zA-Z0-9._:%-]+)/post/([a-zA-Z0-9]+)`)

type ParsedBlueskyURL struct {
	Authority   string
	Rkey        string
	OriginalURL string
}

type ImageInfo struct {
	Fullsize string
	Thumb    string
	Alt      string
}

type VideoInfo struct {
	DirectURL    string // com.atproto.sync.getBlob URL for the raw MP4
	ThumbnailURL string
	Alt          string
}

type PostData struct {
	Images []ImageInfo
	Video  *VideoInfo
	Text   string
}

type BlueskyClient struct {
	xrpc      *xrpc.Client
	directory identity.Directory
}

func NewBlueskyClient() *BlueskyClient {
	return &BlueskyClient{
		xrpc:      &xrpc.Client{Host: "https://public.api.bsky.app"},
		directory: identity.DefaultDirectory(),
	}
}

func ParseBlueskyURL(text string) (*ParsedBlueskyURL, error) {
	m := bskyURLRegex.FindStringSubmatch(text)
	if m == nil {
		return nil, fmt.Errorf("no Bluesky post URL found")
	}
	return &ParsedBlueskyURL{
		Authority:   m[1],
		Rkey:        m[2],
		OriginalURL: m[0],
	}, nil
}

func (c *BlueskyClient) ResolveToDID(ctx context.Context, authority string) (string, error) {
	if len(authority) > 4 && authority[:4] == "did:" {
		return authority, nil
	}
	h, err := syntax.ParseHandle(authority)
	if err != nil {
		return "", fmt.Errorf("invalid handle %q: %w", authority, err)
	}
	ident, err := c.directory.LookupHandle(ctx, h)
	if err != nil {
		return "", fmt.Errorf("resolve handle %q: %w", authority, err)
	}
	return ident.DID.String(), nil
}

func (c *BlueskyClient) FetchPost(ctx context.Context, atURI string) (*PostData, error) {
	resp, err := bsky.FeedGetPosts(ctx, c.xrpc, []string{atURI})
	if err != nil {
		return nil, fmt.Errorf("fetch post: %w", err)
	}
	if len(resp.Posts) == 0 {
		return nil, fmt.Errorf("post not found")
	}
	post := resp.Posts[0]
	data := &PostData{
		Images: extractImages(post),
		Video:  extractVideo(post),
	}
	if feedPost, ok := post.Record.Val.(*bsky.FeedPost); ok {
		data.Text = feedPost.Text
	}
	return data, nil
}

func extractImages(post *bsky.FeedDefs_PostView) []ImageInfo {
	if post.Embed == nil {
		return nil
	}
	var imgs []ImageInfo
	if iv := post.Embed.EmbedImages_View; iv != nil {
		for _, img := range iv.Images {
			imgs = append(imgs, ImageInfo{
				Fullsize: img.Fullsize,
				Thumb:    img.Thumb,
				Alt:      img.Alt,
			})
		}
	}
	if rwm := post.Embed.EmbedRecordWithMedia_View; rwm != nil && rwm.Media != nil {
		if iv := rwm.Media.EmbedImages_View; iv != nil {
			for _, img := range iv.Images {
				imgs = append(imgs, ImageInfo{
					Fullsize: img.Fullsize,
					Thumb:    img.Thumb,
					Alt:      img.Alt,
				})
			}
		}
	}
	return imgs
}

func extractVideo(post *bsky.FeedDefs_PostView) *VideoInfo {
	feedPost, ok := post.Record.Val.(*bsky.FeedPost)
	if !ok || feedPost.Embed == nil {
		return nil
	}

	var embedVideo *bsky.EmbedVideo
	if feedPost.Embed.EmbedVideo != nil {
		embedVideo = feedPost.Embed.EmbedVideo
	} else if feedPost.Embed.EmbedRecordWithMedia != nil && feedPost.Embed.EmbedRecordWithMedia.Media != nil {
		embedVideo = feedPost.Embed.EmbedRecordWithMedia.Media.EmbedVideo
	}
	if embedVideo == nil || embedVideo.Video == nil {
		return nil
	}

	did := post.Author.Did
	cid := embedVideo.Video.Ref.String()
	directURL := fmt.Sprintf("https://bsky.social/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", did, cid)

	vi := &VideoInfo{DirectURL: directURL}
	if embedVideo.Alt != nil {
		vi.Alt = *embedVideo.Alt
	}
	// Thumbnail from the view if available
	if post.Embed != nil {
		if vv := post.Embed.EmbedVideo_View; vv != nil && vv.Thumbnail != nil {
			vi.ThumbnailURL = *vv.Thumbnail
		} else if rwm := post.Embed.EmbedRecordWithMedia_View; rwm != nil && rwm.Media != nil {
			if vv := rwm.Media.EmbedVideo_View; vv != nil && vv.Thumbnail != nil {
				vi.ThumbnailURL = *vv.Thumbnail
			}
		}
	}
	return vi
}
