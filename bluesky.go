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
var atURIRegex = regexp.MustCompile(`at://(did:[a-z]+:[a-zA-Z0-9._:%-]+)/app\.bsky\.feed\.post/([a-zA-Z0-9]+)`)

type ParsedMediaURL struct {
	Source      string // "bluesky" or "inkbunny"
	Authority   string
	Rkey        string
	OriginalURL string
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

func ParseMediaURL(text string) (*ParsedMediaURL, error) {
	if parsed, err := ParseInkbunnyURL(text); err == nil {
		return parsed, nil
	}
	if parsed, err := ParseTwitterURL(text); err == nil {
		return parsed, nil
	}
	if m := atURIRegex.FindStringSubmatch(text); m != nil {
		return &ParsedMediaURL{
			Source:      "bluesky",
			Authority:   m[1],
			Rkey:        m[2],
			OriginalURL: m[0],
		}, nil
	}
	m := bskyURLRegex.FindStringSubmatch(text)
	if m == nil {
		return nil, fmt.Errorf("no supported post URL found")
	}
	return &ParsedMediaURL{
		Source:      "bluesky",
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

func (c *BlueskyClient) FetchPost(ctx context.Context, atURI string) (*MediaResult, error) {
	resp, err := bsky.FeedGetPosts(ctx, c.xrpc, []string{atURI})
	if err != nil {
		return nil, fmt.Errorf("fetch post: %w", err)
	}
	if len(resp.Posts) == 0 {
		return nil, fmt.Errorf("post not found")
	}
	post := resp.Posts[0]
	data := &MediaResult{
		Images: extractImages(post),
		Video:  extractVideo(post),
	}
	if feedPost, ok := post.Record.Val.(*bsky.FeedPost); ok {
		data.Text = feedPost.Text
	}
	return data, nil
}

func extractImages(post *bsky.FeedDefs_PostView) []MediaImage {
	if post.Embed == nil {
		return nil
	}
	var imgs []MediaImage
	if iv := post.Embed.EmbedImages_View; iv != nil {
		for _, img := range iv.Images {
			imgs = append(imgs, MediaImage{
				Fullsize: img.Fullsize,
				Thumb:    img.Thumb,
				Alt:      img.Alt,
			})
		}
	}
	if rwm := post.Embed.EmbedRecordWithMedia_View; rwm != nil && rwm.Media != nil {
		if iv := rwm.Media.EmbedImages_View; iv != nil {
			for _, img := range iv.Images {
				imgs = append(imgs, MediaImage{
					Fullsize: img.Fullsize,
					Thumb:    img.Thumb,
					Alt:      img.Alt,
				})
			}
		}
	}
	return imgs
}

func extractVideo(post *bsky.FeedDefs_PostView) *MediaVideo {
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

	vi := &MediaVideo{DirectURL: directURL}
	if embedVideo.Alt != nil {
		vi.Alt = *embedVideo.Alt
	}
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
