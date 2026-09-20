package linkparser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tt_worker/internal/model"
)

func TestParseLegacyToutiaoLinks(t *testing.T) {
	tests := map[string]string{
		"https://www.toutiao.com/article/7556637402098450963/?x=1": "7556637402098450963",
		"https://www.toutiao.com/w/1846492016803840/":              "1846492016803840",
		"https://www.toutiao.com/video/7564607673153355839/":       "7564607673153355839",
		"https://www.toutiao.com/item/7582514158952874538/":        "7582514158952874538",
		"7582514158952874538":                                      "7582514158952874538",
	}
	for raw, want := range tests {
		if got := Parse(raw); got.ObjectID != want || got.ItemID != want {
			t.Fatalf("Parse(%q)=%+v want=%s", raw, got, want)
		}
	}
}

func TestTaskCandidateOrderMatchesLegacy(t *testing.T) {
	task := model.Task{
		PostID: "post",
		ExternalAPIData: model.ExternalAPIData{
			BuyParams: []model.BuyParam{{Value: "buy-1"}, {Value: "buy-2"}},
			Parameter: "parameter",
		},
	}
	got := taskCandidates(task)
	want := []string{"post", "buy-1", "buy-2", "parameter"}
	if len(got) != len(want) {
		t.Fatalf("candidates=%v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("candidates=%v", got)
		}
	}
}

func TestResolveTaskFollowsToutiaoShortLink(t *testing.T) {
	const target = "https://www.toutiao.com/article/7556637402098450963/"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", target)
		writer.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resolution, err := resolveTask(
		context.Background(),
		model.Task{PostID: server.URL + "/is/test/"},
		client,
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Target.ObjectID != "7556637402098450963" || resolution.ResolvedURL != target {
		t.Fatalf("resolution=%+v", resolution)
	}
}

func TestResolveTaskFallsBackToBuyParams(t *testing.T) {
	task := model.Task{
		PostID:          "not-a-target",
		ExternalAPIData: model.ExternalAPIData{BuyParams: []model.BuyParam{{Value: "https://www.toutiao.com/video/7564607673153355839/"}}},
	}
	resolution, err := resolveTask(context.Background(), task, http.DefaultClient, allowedTTHost)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Target.ObjectID != "7564607673153355839" {
		t.Fatalf("resolution=%+v", resolution)
	}
}
