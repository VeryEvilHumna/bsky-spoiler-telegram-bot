package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	bbURLWithAttrRe = regexp.MustCompile(`(?i)\[url=([^\]]+)\](.*?)\[/url\]`)
	bbURLPlainRe    = regexp.MustCompile(`(?i)\[url\](.*?)\[/url\]`)
	bbImgRe         = regexp.MustCompile(`(?i)\[img\](.*?)\[/img\]`)
	bbListItemRe    = regexp.MustCompile(`(?i)\[\*\]`)
	bbListOpenRe    = regexp.MustCompile(`(?i)\[list(?:=([^\]]*))?\]`)
	bbListCloseRe   = regexp.MustCompile(`(?i)\[/list\]`)
	bbUlOpenRe      = regexp.MustCompile(`(?i)\[ul\]`)
	bbUlCloseRe     = regexp.MustCompile(`(?i)\[/ul\]`)
	bbOlOpenRe      = regexp.MustCompile(`(?i)\[ol\]`)
	bbOlCloseRe     = regexp.MustCompile(`(?i)\[/ol\]`)
)

type bbTagDef struct {
	openHTML  string
	closeHTML string
}

var bbSimpleTags = map[string]bbTagDef{
	"b":       {openHTML: "<b>", closeHTML: "</b>"},
	"i":       {openHTML: "<i>", closeHTML: "</i>"},
	"u":       {openHTML: "<u>", closeHTML: "</u>"},
	"s":       {openHTML: "<s>", closeHTML: "</s>"},
	"strike":  {openHTML: "<s>", closeHTML: "</s>"},
	"del":     {openHTML: "<s>", closeHTML: "</s>"},
	"spoiler": {openHTML: `<span class="tg-spoiler">`, closeHTML: "</span>"},
	"code":    {openHTML: "<code>", closeHTML: "</code>"},
	"pre":     {openHTML: "<pre>", closeHTML: "</pre>"},
	"quote":   {openHTML: "<blockquote>", closeHTML: "</blockquote>"},
}

var bbStripAttrTags = []string{"color", "size", "font"}

var bbStripSimpleTags = []string{"center", "left", "right", "highlight", "heading"}

type bbCompiledTag struct {
	open  *regexp.Regexp
	close *regexp.Regexp
}

var bbSimpleTagRegexes = func() map[string]bbCompiledTag {
	m := make(map[string]bbCompiledTag)
	for tag := range bbSimpleTags {
		m[tag] = bbCompiledTag{
			open:  regexp.MustCompile(`(?i)\[` + tag + `\]`),
			close: regexp.MustCompile(`(?i)\[\/` + tag + `\]`),
		}
	}
	return m
}()

var bbStripAttrRegexes = func() []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(bbStripAttrTags))
	for i, tag := range bbStripAttrTags {
		res[i] = regexp.MustCompile(`(?i)\[` + tag + `=[^\]]+\](.*?)\[/` + tag + `\]`)
	}
	return res
}()

var bbStripSimpleRegexes = func() []bbCompiledTag {
	res := make([]bbCompiledTag, len(bbStripSimpleTags))
	for i, tag := range bbStripSimpleTags {
		res[i] = bbCompiledTag{
			open:  regexp.MustCompile(`(?i)\[` + tag + `\]`),
			close: regexp.MustCompile(`(?i)\[\/` + tag + `\]`),
		}
	}
	return res
}()

// ConvertBBCodeToTelegram converts BB code markup to Telegram-compatible HTML.
func ConvertBBCodeToTelegram(bbcode string) string {
	s := html.EscapeString(bbcode)

	s = processLists(s)

	for bbTag, def := range bbSimpleTags {
		re := bbSimpleTagRegexes[bbTag]
		if re.open.MatchString(s) && re.close.MatchString(s) {
			s = re.open.ReplaceAllString(s, def.openHTML)
			s = re.close.ReplaceAllString(s, def.closeHTML)
		}
	}

	s = bbURLWithAttrRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := bbURLWithAttrRe.FindStringSubmatch(match)
		href := html.UnescapeString(parts[1])
		text := parts[2]
		return fmt.Sprintf("<a href=\"%s\">%s</a>", href, text)
	})

	s = bbURLPlainRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := bbURLPlainRe.FindStringSubmatch(match)
		url := html.UnescapeString(parts[1])
		return fmt.Sprintf("<a href=\"%s\">%s</a>", url, parts[1])
	})

	for i := range bbStripAttrTags {
		s = bbStripAttrRegexes[i].ReplaceAllString(s, "$1")
	}

	for i := range bbStripSimpleTags {
		re := bbStripSimpleRegexes[i]
		s = re.open.ReplaceAllString(s, "")
		s = re.close.ReplaceAllString(s, "")
	}

	s = strings.ReplaceAll(s, "[hr]", "───")
	s = strings.ReplaceAll(s, "[HR]", "───")

	s = bbImgRe.ReplaceAllString(s, "$1")

	return s
}

func processLists(s string) string {
	s = bbListOpenRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := bbListOpenRe.FindStringSubmatch(match)
		listType := ""
		if len(parts) > 1 {
			listType = parts[1]
		}
		return "\x00LIST:" + listType + "\x00"
	})
	s = bbListCloseRe.ReplaceAllString(s, "\x00/LIST\x00")

	s = bbUlOpenRe.ReplaceAllString(s, "\x00LIST:\x00")
	s = bbUlCloseRe.ReplaceAllString(s, "\x00/LIST\x00")

	s = bbOlOpenRe.ReplaceAllString(s, "\x00LIST:1\x00")
	s = bbOlCloseRe.ReplaceAllString(s, "\x00/LIST\x00")

	listBlockRe := regexp.MustCompile(`(?s)\x00LIST:([^\x00]*?)\x00(.*?)\x00/LIST\x00`)
	for listBlockRe.MatchString(s) {
		s = listBlockRe.ReplaceAllStringFunc(s, func(match string) string {
			parts := listBlockRe.FindStringSubmatch(match)
			return convertList(parts[2], parts[1])
		})
	}

	return s
}

func convertList(content, listType string) string {
	content = bbListItemRe.ReplaceAllString(content, "\x00ITEM\x00")

	rawItems := strings.Split(content, "\x00ITEM\x00")

	var items []string
	for i, item := range rawItems {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" && i == 0 {
			continue
		}
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}

	var b strings.Builder
	for i, item := range items {
		if listType == "" {
			fmt.Fprintf(&b, "\n• %s", item)
		} else if listType == "1" {
			fmt.Fprintf(&b, "\n%d. %s", i+1, item)
		} else if len(listType) == 1 {
			ch := listType[0]
			if ch >= 'A' && ch <= 'Z' {
				letter := ch + byte(i)
				if letter > 'Z' {
					letter = 'Z'
				}
				fmt.Fprintf(&b, "\n%c. %s", letter, item)
			} else if ch >= 'a' && ch <= 'z' {
				letter := ch + byte(i)
				if letter > 'z' {
					letter = 'z'
				}
				fmt.Fprintf(&b, "\n%c. %s", letter, item)
			} else {
				fmt.Fprintf(&b, "\n• %s", item)
			}
		} else {
			fmt.Fprintf(&b, "\n• %s", item)
		}
	}

	return b.String()
}
