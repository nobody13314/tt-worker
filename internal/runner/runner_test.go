package runner

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tt_worker/internal/business"
	"tt_worker/internal/model"
	"tt_worker/internal/signer"
)

type fakeSigner struct{ url string }

func (s fakeSigner) Sign(context.Context, signer.Input) (signer.Output, error) {
	return signer.Output{URL: s.url, Header: map[string]string{"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8"}}, nil
}

func TestExecuteSignedRequest(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer target.Close()
	handler, _ := business.New("tt_dz")
	r := Runner{Handler: handler, Signer: fakeSigner{url: target.URL}}
	if err := r.Execute(context.Background(), target.Client(), model.Device{}, model.Target{ObjectID: "7472691484250800664", ContentType: model.ContentTypeUGCVideo}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteLogsBusinessIDDeviceIDsURLAndResponse(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer targetServer.Close()

	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}()

	handler, _ := business.New("tt_dz")
	r := Runner{
		Handler:   handler,
		Signer:    fakeSigner{url: targetServer.URL + "/send?iid=iid-123&device_id=did-456"},
		LogDetail: "all",
	}
	err := r.Execute(
		context.Background(),
		targetServer.Client(),
		model.Device{"iid": "iid-123", "device_id": "did-456"},
		model.Target{ObjectID: "7472691484250800664", ContentType: model.ContentTypeUGCVideo},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"business=tt_dz",
		"business_id=7472691484250800664",
		"iid=iid-123",
		"device_id=did-456",
		"url=" + targetServer.URL + "/send?iid=iid-123&device_id=did-456",
		"success=true",
		`response="{\"code\":0}"`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("log missing %q: %s", expected, output.String())
		}
	}
}
