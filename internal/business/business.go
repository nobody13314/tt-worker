package business

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"tt_worker/internal/model"
	"tt_worker/internal/signer"
)

type Handler interface {
	Name() string
	TaskTypeID() int
	Build(model.Device, model.Target) (signer.Input, []byte, error)
	Validate(int, []byte) error
}

func New(name string) (Handler, error) {
	if strings.EqualFold(name, "tt_dz") {
		return action{name: "tt_dz", taskType: 38}, nil
	}
	return nil, fmt.Errorf("unknown business %q", name)
}

type action struct {
	name     string
	taskType int
}

func (a action) Name() string    { return a.name }
func (a action) TaskTypeID() int { return a.taskType }

func (a action) Build(dev model.Device, target model.Target) (signer.Input, []byte, error) {
	return buildTT(dev, target)
}

// buildTT preserves the legacy tt-dz request byte-for-byte. Its ticket fields
// remain fixed because request semantics are outside this infrastructure move.
func buildTT(dev model.Device, target model.Target) (signer.Input, []byte, error) {
	const endpoint = "https://api5-normal-lq.toutiaoapi.com/action/api/v1/do_action/"
	targetID := strings.TrimSpace(target.ObjectID)
	if targetID == "" {
		return signer.Input{}, nil, fmt.Errorf("target_id is required")
	}

	params := map[string]string{
		"device_platform": "android", "os": "android", "ssmix": "a", "_rticket": "1756894316302",
		"cdid":    str(dev, "cdid", "5a7d99dd-6fba-4edb-876a-729f164f3694"),
		"channel": str(dev, "channel", "vivo_13_64"), "aid": "13", "app_name": "news_article",
		"version_code": str(dev, "version_code", "140000"), "version_name": str(dev, "version_name", "14.0.0"),
		"manifest_version_code": str(dev, "manifest_version_code", "14000"),
		"update_version_code":   str(dev, "update_version_code", "140006"),
		"resolution":            str(dev, "resolution", "1080*2028"), "dpi": str(dev, "dpi", "440"),
		"device_type": str(dev, "device_type", "Pixel+3"), "device_brand": str(dev, "device_brand", "google"),
		"language": "zh", "os_api": str(dev, "os_api", "30"), "os_version": str(dev, "os_version", "11"), "ac": "wifi",
		"current_launch_mode": "enter_launch", "current_launch_mode_hot": "enter_launch", "today_first_launch_mode": "enter_launch",
		"dq_param": "0", "isTTWebViewHeifSupport": "0", "plugin": "0", "openlive_plugin_status": "1",
		"client_vid": str(dev, "client_vid", "14082827,13994498,12234153,13812938,14172946,13599184"),
		"session_id": str(dev, "session_id", "d55f475e-2087-4350-a67a-6229ed718e04"),
		"host_abi":   str(dev, "host_abi", "arm64-v8a"), "rom_version": str(dev, "rom_version", "30"),
		"iid": str(dev, "iid", "986740712068778"), "device_id": str(dev, "device_id", "3555135372927264"),
	}
	payload := map[string]string{
		"action_type": "0",
		"busi_value":  `{"category":"profile_article"}`,
		"target_id":   targetID,
		"biz_id":      "0",
		"is_cancel":   "0",
	}
	form := url.Values{}
	for key, value := range payload {
		form.Set(key, value)
	}
	body := []byte(form.Encode())
	header := map[string]string{
		"User-Agent":      str(dev, "ua", "com.ss.android.article.news/14000 (Linux; U; Android 11; zh_CN_#Hans; Pixel 3; Build/RP1A.200720.009; Cronet/TTNetVersion:fc4cebd3 2024-12-10 QuicVersion:d9628e3d 2024-10-11)"),
		"x-ss-req-ticket": "1756894316304", "sdk-version": "2", "passport-sdk-version": "505109",
		"x-vc-bdturing-sdk-version": "4.0.3.cn", "x-tt-request-tag": "n=0",
		"x-tt-store-region": "cn-gz", "x-tt-store-region-src": "did", "x-ss-dp": "13",
		"Content-Type": "application/x-www-form-urlencoded",
	}
	return signer.Input{URL: endpoint, Params: params, Devices: dev, Data: string(body), Header: header}, body, nil
}

func (a action) Validate(status int, raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("tt_dz empty response, http %d", status)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("tt_dz decode response: %w", err)
	}
	if code, ok := result["code"].(float64); ok {
		message, _ := result["msg"].(string)
		if int(code) == 0 || strings.Contains(strings.ToLower(message), "success") {
			return nil
		}
		return fmt.Errorf("tt_dz rejected code=%v msg=%s", code, message)
	}
	if base, ok := result["BaseResp"].(map[string]any); ok {
		message, _ := base["StatusMessage"].(string)
		if strings.EqualFold(message, "success") {
			return nil
		}
		return fmt.Errorf("tt_dz rejected status=%s", message)
	}
	return fmt.Errorf("tt_dz unexpected response: %.300s", raw)
}

func str(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}
