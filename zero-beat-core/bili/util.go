package bili

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var emTag = regexp.MustCompile(`<[^>]+>`)

func jsonUnmarshal(data []byte, out any) error { return json.Unmarshal(data, out) }

// stripEm 去掉搜索结果里的高亮标签与 HTML 实体。
func stripEm(s string) string {
	s = emTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return strings.TrimSpace(s)
}

// parseClock 把「mm:ss」或「hh:mm:ss」解析成秒。
func parseClock(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// normalizeURL 补全协议相对地址。
func normalizeURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

func isRiskControl(err error) bool {
	var ae *APIError
	if !asAPIError(err, &ae) {
		return false
	}
	return ae.Code == -403 || ae.Code == -412 || ae.Code == -352
}

// ParseBVID 从 BV 号、av 号或视频链接里提取 BV 号。
func ParseBVID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errInvalid("空输入")
	}
	if u, err := url.Parse(input); err == nil && u.Host != "" {
		for _, segment := range strings.Split(strings.Trim(u.Path, "/"), "/") {
			if strings.HasPrefix(segment, "BV") && len(segment) == 12 {
				return segment, nil
			}
		}
		if q := u.Query().Get("bvid"); q != "" {
			return q, nil
		}
		return "", errInvalid("链接里找不到 BV 号")
	}
	if strings.HasPrefix(input, "BV") && len(input) == 12 {
		return input, nil
	}
	if strings.Contains(input, "BV") {
		idx := strings.Index(input, "BV")
		candidate := input[idx:]
		if len(candidate) >= 12 {
			return candidate[:12], nil
		}
	}
	return "", errInvalid("看起来不是 BV 号")
}

type invalidInput string

func (e invalidInput) Error() string { return "bilibili: " + string(e) }

func errInvalid(msg string) error { return invalidInput(msg) }
