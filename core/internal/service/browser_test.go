package service

import (
	"context"
	"testing"

	"github.com/coder/websocket"
)

/* 后端按轮的来源端分工：桥只在桌面端自己发起的轮里用。 */
func TestBrowserBackendByTurnClient(t *testing.T) {
	s := NewBrowserService(t.TempDir())
	desktop := context.WithValue(context.Background(), turnClientKey{}, "desktop")
	web := context.WithValue(context.Background(), turnClientKey{}, "web")

	if s.hd(desktop) == nil {
		t.Fatal("桥未连接时应走无头")
	}
	s.conn = &websocket.Conn{} // 假桥：只验证分工判断，不发帧
	if s.hd(desktop) != nil {
		t.Fatal("桌面端的轮应走桥")
	}
	if s.hd(web) == nil {
		t.Fatal("web 的轮应走无头")
	}
	if s.hd(context.Background()) == nil {
		t.Fatal("后台轮应走无头")
	}
}
