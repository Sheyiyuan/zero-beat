package bili

import (
	"context"
	"fmt"
	"net/url"
)

// SubtitleLine 是一句字幕（同时作为歌词）。
type SubtitleLine struct {
	StartMS int
	EndMS   int
	Text    string
}

type subtitleBody struct {
	Body []struct {
		From    float64 `json:"from"`
		To      float64 `json:"to"`
		Content string  `json:"content"`
	} `json:"body"`
}

// Subtitles 拉取视频的人工/自动字幕，作为歌词来源。
func (c *Client) Subtitles(ctx context.Context, bvid string, cid int64) ([]SubtitleLine, error) {
	params := url.Values{
		"bvid": {bvid},
		"cid":  {fmt.Sprint(cid)},
	}
	var data struct {
		Subtitle struct {
			Subtitles []struct {
				Lan         string `json:"lan"`
				LanDoc      string `json:"lan_doc"`
				SubtitleURL string `json:"subtitle_url"`
				IsLock      bool   `json:"is_lock"`
			} `json:"subtitles"`
		} `json:"subtitle"`
	}
	if err := c.getJSON(ctx, withQuery(apiHost+"/x/player/v2", params), videoReferer(bvid), &data); err != nil {
		return nil, err
	}
	subs := data.Subtitle.Subtitles
	if len(subs) == 0 {
		return nil, nil
	}
	// 优先中文字幕。
	chosen := subs[0]
	for _, s := range subs {
		if s.Lan == "zh-CN" || s.Lan == "zh-Hans" {
			chosen = s
			break
		}
	}
	if chosen.SubtitleURL == "" {
		return nil, nil
	}
	var body subtitleBody
	if err := c.getJSON(ctx, normalizeURL(chosen.SubtitleURL), videoReferer(bvid), &body); err != nil {
		return nil, err
	}
	lines := make([]SubtitleLine, 0, len(body.Body))
	for _, l := range body.Body {
		if l.Content == "" {
			continue
		}
		lines = append(lines, SubtitleLine{
			StartMS: int(l.From * 1000),
			EndMS:   int(l.To * 1000),
			Text:    l.Content,
		})
	}
	return lines, nil
}
