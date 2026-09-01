package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/utils"
	tele "gopkg.in/telebot.v3"
)

// imageURL/imageToken 由 main 启动时经 SetImageEndpoint 注入，指向本地 GPU 生图 worker。
var (
	imageURL   string
	imageToken string
)

// imageClient：生图较慢（十几秒~几十秒），用长超时。
var imageClient = &http.Client{Timeout: 180 * time.Second}

// SetImageEndpoint 注入生图 worker 端点（url 空则 /gen 提示未启用）。
func SetImageEndpoint(url, token string) {
	imageURL, imageToken = url, token
}

// Gen 处理 /gen <描述>：把提示词发给本地 GPU 生图 worker，取回图片作为 Photo 发出（附提示词）。
func Gen(c tele.Context) error {
	prompt := strings.TrimSpace(c.Message().Payload)
	if imageURL == "" {
		return c.Send("生图未启用（服务端未配置 IMAGE_URL）")
	}
	if prompt == "" {
		return c.Send("用法：/gen <描述>\n例：/gen a cyberpunk city at night, neon, rain")
	}
	log.Infof("sender:[%d - %s] /gen %q", c.Sender().ID, c.Sender().FirstName, prompt)

	_ = c.Notify(tele.UploadingPhoto)
	ph, _ := c.Bot().Send(c.Chat(), "🎨 生成中…（十几秒~几十秒）")

	img, err := requestImage(prompt)
	if err != nil {
		log.Warnf("/gen 失败: %v", err)
		return updatePlaceholder(c, ph, "生图失败 🥲\n原因："+err.Error())
	}

	if ph != nil {
		_ = c.Bot().Delete(ph)
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(img)), Caption: prompt}
	_, err = c.Bot().Send(c.Chat(), photo)
	return err
}

// requestImage POST prompt 到 worker，返回图片字节（worker 直接回 image/png）。
func requestImage(prompt string) ([]byte, error) {
	body, _ := json.Marshal(map[string]any{"prompt": prompt})
	req, err := http.NewRequest(http.MethodPost, imageURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if imageToken != "" {
		req.Header.Set("X-Auth-Token", imageToken)
	}
	resp, err := imageClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("worker %d: %s", resp.StatusCode, utils.Truncate(string(data), 150))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("worker 返回空图片")
	}
	return data, nil
}
