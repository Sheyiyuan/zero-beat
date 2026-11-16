package bili

import (
	"context"
	"fmt"
	"net/url"
)

// QRCode 是一次扫码登录的二维码。
type QRCode struct {
	URL string
	Key string
}

// LoginStatus 是二维码的轮询结果。
type LoginStatus struct {
	Code     int
	Message  string
	LoggedIn bool
}

// StartQRLogin 申请登录二维码。
func (c *Client) StartQRLogin(ctx context.Context) (QRCode, error) {
	var data struct {
		URL       string `json:"url"`
		QRCodeKey string `json:"qrcode_key"`
	}
	endpoint := passportHost + "/x/passport-login/web/qrcode/generate?source=main-fe-header"
	if err := c.getJSON(ctx, endpoint, "https://www.bilibili.com", &data); err != nil {
		return QRCode{}, err
	}
	if data.URL == "" || data.QRCodeKey == "" {
		return QRCode{}, fmt.Errorf("bilibili: 二维码生成失败")
	}
	return QRCode{URL: data.URL, Key: data.QRCodeKey}, nil
}

// PollQRLogin 轮询二维码状态，成功时自动写入 cookie。
func (c *Client) PollQRLogin(ctx context.Context, key string) (LoginStatus, error) {
	params := url.Values{"qrcode_key": {key}, "source": {"main-fe-header"}}
	var data struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	endpoint := withQuery(passportHost+"/x/passport-login/web/qrcode/poll", params)
	if err := c.getJSON(ctx, endpoint, "https://www.bilibili.com", &data); err != nil {
		return LoginStatus{}, err
	}
	status := LoginStatus{Code: data.Code, Message: data.Message, LoggedIn: data.Code == 0}
	if !status.LoggedIn && status.Message == "" {
		switch data.Code {
		case 86101:
			status.Message = "等待扫码"
		case 86090:
			status.Message = "已扫码，等待确认"
		case 86038:
			status.Message = "二维码已失效"
		default:
			status.Message = fmt.Sprintf("状态码 %d", data.Code)
		}
	}
	return status, nil
}

// Logout 清除登录态。
func (c *Client) Logout() {
	c.SetCookies(nil)
	c.setUser(User{})
}
