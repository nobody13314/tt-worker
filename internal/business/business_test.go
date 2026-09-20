package business

import (
	"net/url"
	"testing"

	"tt_worker/internal/model"
)

func TestNewTT(t *testing.T) {
	handler, err := New("tt_dz")
	if err != nil || handler.Name() != "tt_dz" || handler.TaskTypeID() != 38 {
		t.Fatalf("handler=%v err=%v", handler, err)
	}
	if _, err := New("dcd_dz"); err == nil {
		t.Fatal("DCD business unexpectedly accepted")
	}
}

func TestTTRequestMatchesLegacyContract(t *testing.T) {
	handler, _ := New("tt_dz")
	device := model.Device{
		"iid": "986740712068778", "device_id": "3555135372927264",
		"cdid": "test-cdid", "channel": "vivo_13_64", "ua": "legacy-user-agent",
	}
	input, body, err := handler.Build(device, model.Target{ObjectID: "7556637402098450963"})
	if err != nil {
		t.Fatal(err)
	}
	if input.URL != "https://api5-normal-lq.toutiaoapi.com/action/api/v1/do_action/" {
		t.Fatalf("URL=%s", input.URL)
	}
	wantBody := "action_type=0&biz_id=0&busi_value=%7B%22category%22%3A%22profile_article%22%7D&is_cancel=0&target_id=7556637402098450963"
	if string(body) != wantBody {
		t.Fatalf("body=%s\nwant=%s", body, wantBody)
	}
	if input.Data != wantBody {
		t.Fatalf("signing data=%v", input.Data)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("target_id") != "7556637402098450963" || form.Get("busi_value") != `{"category":"profile_article"}` {
		t.Fatalf("form=%v", form)
	}
	for key, value := range map[string]string{
		"_rticket": "1756894316302", "aid": "13", "app_name": "news_article",
		"iid": "986740712068778", "device_id": "3555135372927264",
	} {
		if input.Params[key] != value {
			t.Errorf("param %s=%q want=%q", key, input.Params[key], value)
		}
	}
	for key, value := range map[string]string{
		"User-Agent": "legacy-user-agent", "x-ss-req-ticket": "1756894316304",
		"sdk-version": "2", "passport-sdk-version": "505109",
		"x-vc-bdturing-sdk-version": "4.0.3.cn", "x-ss-dp": "13",
		"Content-Type": "application/x-www-form-urlencoded",
	} {
		if input.Header[key] != value {
			t.Errorf("header %s=%q want=%q", key, input.Header[key], value)
		}
	}
}

func TestTTValidate(t *testing.T) {
	handler, _ := New("tt_dz")
	for _, test := range []struct {
		name string
		body string
		ok   bool
	}{
		{"code zero", `{"code":0,"msg":"ok"}`, true},
		{"success message", `{"code":1,"msg":"SUCCESS"}`, true},
		{"base response", `{"BaseResp":{"StatusMessage":"success"}}`, true},
		{"code failure", `{"code":1,"msg":"denied"}`, false},
		{"empty", ``, false},
		{"unknown", `{"status":0}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := handler.Validate(200, []byte(test.body))
			if (err == nil) != test.ok {
				t.Fatalf("err=%v want success=%t", err, test.ok)
			}
		})
	}
}
