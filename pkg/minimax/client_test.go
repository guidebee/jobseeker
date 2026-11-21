package minimax

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	c := NewClient("test-key")
	c.BaseURL = server.URL
	return c
}

func TestSendMessageReturnsTheAnswer(t *testing.T) {
	c := serve(t, 200, `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}],"base_resp":{"status_code":0,"status_msg":""}}`)
	got, err := c.SendMessage("hi")
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// MiniMax reports a rejected key or an empty balance as HTTP 200 with base_resp and no choices; the real reason must surface.
func TestSendMessageSurfacesTheAPIErrorInsideAnHTTP200(t *testing.T) {
	c := serve(t, 200, `{"choices":null,"base_resp":{"status_code":1004,"status_msg":"login fail: invalid api key"}}`)
	_, err := c.SendMessage("hi")
	if err == nil || !strings.Contains(err.Error(), "1004") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "no content") {
		t.Fatalf("the error hides the cause: %v", err)
	}
}

func TestSendMessageReportsAnEmptyAnswerAndNoChoices(t *testing.T) {
	c := serve(t, 200, `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`)
	if _, err := c.SendMessage("hi"); err == nil || !strings.Contains(err.Error(), "empty answer") {
		t.Fatalf("error = %v", err)
	}
	c = serve(t, 200, `{"choices":[]}`)
	if _, err := c.SendMessage("hi"); err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("error = %v", err)
	}
}

func TestSendMessageReportsNonOKStatus(t *testing.T) {
	c := serve(t, 500, `server down`)
	if _, err := c.SendMessage("hi"); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v", err)
	}
}

func TestModelDefaultsToM1AndMINIMAX_MODELOverridesIt(t *testing.T) {
	t.Setenv("MINIMAX_MODEL", "")
	if got := NewClient("k").Model; got != "MiniMax-M1" {
		t.Fatalf("default model = %q", got)
	}
	t.Setenv("MINIMAX_MODEL", "MiniMax-Next")
	if got := NewClient("k").Model; got != "MiniMax-Next" {
		t.Fatalf("overridden model = %q", got)
	}
}
