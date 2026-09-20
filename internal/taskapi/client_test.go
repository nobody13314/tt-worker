package taskapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"task":{"task_id":"t1","total_quantity":30}}`)
	}))
	defer s.Close()
	task, err := New(s.URL, "/get", "/complete", "u", time.Second).Get(context.Background(), 39)
	if err != nil || task.TaskID != "t1" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
}

func TestCompleteSendsFinalResult(t *testing.T) {
	var payload struct {
		CompletedQuantity int        `json:"completed_quantity"`
		Result            Completion `json:"result_data"`
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer s.Close()

	result := Completion{Requested: 30, Attempted: 30, Success: 6, Failed: 24, ProxyRotations: 3}
	if err := New(s.URL, "/get", "/complete", "u", time.Second).Complete(context.Background(), "t1", result); err != nil {
		t.Fatal(err)
	}
	if payload.CompletedQuantity != 6 || payload.Result != result {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestCompleteSendsZeroWhenEveryRequestFailed(t *testing.T) {
	var completedQuantity int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			CompletedQuantity int `json:"completed_quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		completedQuantity = payload.CompletedQuantity
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer s.Close()

	result := Completion{Requested: 30, Attempted: 30, Failed: 30, ProxyRotations: 3}
	if err := New(s.URL, "/get", "/complete", "u", time.Second).Complete(context.Background(), "t1", result); err != nil {
		t.Fatal(err)
	}
	if completedQuantity != 0 {
		t.Fatalf("completed_quantity=%d", completedQuantity)
	}
}
