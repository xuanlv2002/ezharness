package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/*
	发送形态校验：text/files 至少其一、附件个数（路径沙箱校验依赖

Hub，在手测/集成覆盖；两用例均在解 Hub 前返回，nil Svc 安全）。
*/
func TestSendMessageValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &ChatController{Svc: nil}

	post := func(body string) int {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		g.Request.Header.Set("Content-Type", "application/json")
		c.SendMessage(g)
		return w.Code
	}

	if got := post(`{}`); got != http.StatusBadRequest {
		t.Fatalf("empty body: %d", got)
	}
	many, _ := json.Marshal(map[string]any{
		"files": func() []map[string]string {
			out := make([]map[string]string, 9)
			for i := range out {
				out[i] = map[string]string{"name": "a.txt", "path": "C:/w/tmp/att-a.txt"}
			}
			return out
		}(),
	})
	if got := post(string(many)); got != http.StatusBadRequest {
		t.Fatalf("too many files: %d", got)
	}
}
