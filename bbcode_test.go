package main

import "testing"

func TestConvertBBCodeToTelegram(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold",
			input: "[b]hello[/b]",
			want:  "<b>hello</b>",
		},
		{
			name:  "italic",
			input: "[i]hello[/i]",
			want:  "<i>hello</i>",
		},
		{
			name:  "underline",
			input: "[u]hello[/u]",
			want:  "<u>hello</u>",
		},
		{
			name:  "strikethrough with s",
			input: "[s]hello[/s]",
			want:  "<s>hello</s>",
		},
		{
			name:  "strikethrough with strike",
			input: "[strike]hello[/strike]",
			want:  "<s>hello</s>",
		},
		{
			name:  "strikethrough with del",
			input: "[del]hello[/del]",
			want:  "<s>hello</s>",
		},
		{
			name:  "spoiler",
			input: "[spoiler]secret[/spoiler]",
			want:  `<span class="tg-spoiler">secret</span>`,
		},
		{
			name:  "inline code",
			input: "[code]fmt.Println()[/code]",
			want:  "<code>fmt.Println()</code>",
		},
		{
			name:  "pre block",
			input: "[pre]line 1\nline 2[/pre]",
			want:  "<pre>line 1\nline 2</pre>",
		},
		{
			name:  "quote",
			input: "[quote]quoted text[/quote]",
			want:  "<blockquote>quoted text</blockquote>",
		},
		{
			name:  "url with attribute",
			input: "[url=https://example.com]click here[/url]",
			want:  `<a href="https://example.com">click here</a>`,
		},
		{
			name:  "url plain",
			input: "[url]https://example.com[/url]",
			want:  `<a href="https://example.com">https://example.com</a>`,
		},
		{
			name:  "nested bold and italic",
			input: "[b][i]bold italic[/i][/b]",
			want:  "<b><i>bold italic</i></b>",
		},
		{
			name:  "color stripped",
			input: "[color=red]RED[/color]",
			want:  "RED",
		},
		{
			name:  "color hex stripped",
			input: "[color=#00ff00]GREEN[/color]",
			want:  "GREEN",
		},
		{
			name:  "size stripped",
			input: "[size=150]BIG[/size]",
			want:  "BIG",
		},
		{
			name:  "font stripped",
			input: "[font=Arial]styled[/font]",
			want:  "styled",
		},
		{
			name:  "center stripped",
			input: "[center]centered[/center]",
			want:  "centered",
		},
		{
			name:  "left alignment stripped",
			input: "[left]left aligned[/left]",
			want:  "left aligned",
		},
		{
			name:  "right alignment stripped",
			input: "[right]right aligned[/right]",
			want:  "right aligned",
		},
		{
			name:  "highlight stripped",
			input: "[highlight]highlighted[/highlight]",
			want:  "highlighted",
		},
		{
			name:  "heading stripped",
			input: "[heading]Title[/heading]",
			want:  "Title",
		},
		{
			name:  "hr",
			input: "above[hr]below",
			want:  "above───below",
		},
		{
			name:  "img stripped to url",
			input: "[img]https://example.com/pic.jpg[/img]",
			want:  "https://example.com/pic.jpg",
		},
		{
			name:  "unordered list",
			input: "[list]\n[*] item one\n[*] item two\n[/list]",
			want:  "\n• item one\n• item two",
		},
		{
			name:  "ordered list numeric",
			input: "[list=1]\n[*] first\n[*] second\n[/list]",
			want:  "\n1. first\n2. second",
		},
		{
			name:  "ordered list uppercase letter",
			input: "[list=A]\n[*] alpha\n[*] beta\n[/list]",
			want:  "\nA. alpha\nB. beta",
		},
		{
			name:  "ordered list lowercase letter",
			input: "[list=a]\n[*] alpha\n[*] beta\n[/list]",
			want:  "\na. alpha\nb. beta",
		},
		{
			name:  "ul tag",
			input: "[ul]\n[*] one\n[*] two\n[/ul]",
			want:  "\n• one\n• two",
		},
		{
			name:  "ol tag",
			input: "[ol]\n[*] first\n[*] second\n[/ol]",
			want:  "\n1. first\n2. second",
		},
		{
			name:  "html entities escaped",
			input: "foo & bar <baz> \"qux\"",
			want:  "foo &amp; bar &lt;baz&gt; &#34;qux&#34;",
		},
		{
			name:  "html in bbcode escaped",
			input: "[b]<script>alert('xss')</script>[/b]",
			want:  "<b>&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;</b>",
		},
		{
			name:  "case insensitive tags",
			input: "[B]upper[/B] [I]mixed[/i] [U]lower[/u]",
			want:  "<b>upper</b> <i>mixed</i> <u>lower</u>",
		},
		{
			name:  "mixed content",
			input: "Check [b]this[/b] [url=https://example.com]link[/url] and [spoiler]hidden[/spoiler]",
			want:  `Check <b>this</b> <a href="https://example.com">link</a> and <span class="tg-spoiler">hidden</span>`,
		},
		{
			name:  "unclosed tags left as-is",
			input: "[b]no close",
			want:  "[b]no close",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "no bbcode",
			input: "plain text only",
			want:  "plain text only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertBBCodeToTelegram(tt.input)
			if got != tt.want {
				t.Errorf("ConvertBBCodeToTelegram(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}
