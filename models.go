package main

type MessageMetadata struct {
	UserID    int64
	ChatID    int64
	MessageID int
}

type PendingRequest struct {
	HasSpoiler bool
	BotMsgID   int
	CmdMsgID   int
}

type ParsedMediaURL struct {
	Source      string
	Authority   string
	Rkey        string
	OriginalURL string
}

type MediaImage struct {
	Fullsize      string
	Thumb         string
	Alt           string
	NeedsDownload bool
}

type MediaVideo struct {
	DirectURL    string
	ThumbnailURL string
	Alt          string
}

type PendingEmbed struct {
	URL           string
	ChatID        int64
	OriginalMsgID int
	UserID        int64
	UserFirstName string
	UserUsername  string
	MsgText       string
}

type MediaResult struct {
	Images        []MediaImage
	Video         *MediaVideo
	Text          string
	TextIsHTML    bool
	Title         string
	Author        string
	AuthorURL     string
	SubmissionURL string
}
