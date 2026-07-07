package main

import (
	"fmt"
	"html"
)

type CaptionData struct {
	Source        string
	FirstName     string
	Username      string
	OrigFirstName string
	OrigUsername   string
	CWText        string
	HasSpoiler    bool
	PostText      string
	TextIsHTML    bool
	OriginalURL   string
	Title         string
	Author        string
	AuthorURL     string
	SubmissionURL string
}

func buildCaption(d CaptionData) string {
	var body string

	if d.Source == "inkbunny" {
		workLink := fmt.Sprintf(`<a href="%s">%s</a>`, d.SubmissionURL, html.EscapeString(d.Title))
		authorLink := fmt.Sprintf(`<a href="%s">%s</a>`, d.AuthorURL, html.EscapeString(d.Author))
		if d.OrigUsername != "" && d.OrigUsername != d.Username {
			body = fmt.Sprintf(
				`<a href="%s">%s</a> + <a href="%s">%s</a>`+"\n%s by %s",
				"t.me/"+d.OrigUsername,
				"@"+d.OrigUsername,
				"t.me/"+d.Username,
				"@"+d.Username,
				workLink,
				authorLink,
			)
		} else {
			body = fmt.Sprintf(
				`<a href="%s">%s</a> (%s) shared:`+"\n\"<b>%s</b>\" by <b>%s</b>",
				"t.me/"+d.Username,
				d.FirstName,
				"@"+d.Username,
				workLink,
				authorLink,
			)
		}
	} else {
		if d.OrigUsername != "" && d.OrigUsername != d.Username {
			body = fmt.Sprintf(
				`<a href="%s">%s</a> + <a href="%s">%s</a>`+"\n%s",
				"t.me/"+d.OrigUsername,
				"@"+d.OrigUsername,
				"t.me/"+d.Username,
				"@"+d.Username,
				d.OriginalURL,
			)
		} else {
			body = fmt.Sprintf(
				`<a href="%s">%s</a> (%s) shared:`+"\n%s",
				"t.me/"+d.Username,
				d.FirstName,
				"@"+d.Username,
				d.OriginalURL,
			)
		}
	}

	if d.CWText != "" {
		prefix := ""
		if d.HasSpoiler {
			prefix = "CW: "
		}
		body = fmt.Sprintf("<blockquote><b>%s%s</b></blockquote>", prefix, html.EscapeString(d.CWText)) + body
	}

	text := d.PostText
	if text != "" {
		if !d.TextIsHTML {
			text = html.EscapeString(text)
		}
		if d.HasSpoiler {
			label := "Show post text (tap)"
			if d.Source == "inkbunny" {
				label = "Show description (tap)"
			}
			body += fmt.Sprintf(
				"\n<blockquote expandable>⠀\n⠀  <b>%s</b>\n⠀ \n\n%s</blockquote>",
				label, text,
			)
		} else {
			body += fmt.Sprintf("\n<blockquote expandable>%s</blockquote>", text)
		}
	}

	return body
}
