package visionguard

import (
	"context"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/* 记录请求的 provider 桩 */
type stubProvider struct {
	lastReq *types.ModelRequest
}

func (s *stubProvider) Invoke(_ context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	s.lastReq = req
	return &types.ModelResponse{}, nil
}

var png1x1 = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func mkReq() (*types.ModelRequest, *types.Message) {
	imgMsg := &types.Message{
		Role:    types.RoleUser,
		Content: "<image_loaded>\n/tmp/a.png\n</image_loaded>",
		Images:  []types.ImagePart{{MimeType: "image/png", Data: "aGk="}},
	}
	plain := types.Message{Role: types.RoleAssistant, Content: "x"}
	return &types.ModelRequest{Messages: []types.Message{*imgMsg, plain}}, imgMsg
}

func TestStripsWithoutVision(t *testing.T) {
	stub := &stubProvider{}
	p := Warp(func() bool { return false })(nil, stub)
	req, imgMsg := mkReq()
	if _, err := p.Invoke(context.Background(), req); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := stub.lastReq.Messages[0]
	if len(got.Images) != 0 {
		t.Fatalf("images should be stripped, got %d", len(got.Images))
	}
	if want := `<image_loaded omitted="当前模型可能已切换，不支持图片输入">` + "\n/tmp/a.png\n</image_loaded>"; got.Content != want {
		t.Fatalf("content = %q, want %q", got.Content, want)
	}
	// 落盘历史（原消息）不受影响：换回多模态自动恢复
	if len(imgMsg.Images) != 1 || imgMsg.Content != "<image_loaded>\n/tmp/a.png\n</image_loaded>" {
		t.Fatalf("original message mutated: %+v", imgMsg)
	}
}

func TestKeepsWithVision(t *testing.T) {
	stub := &stubProvider{}
	p := Warp(func() bool { return true })(nil, stub)
	req, _ := mkReq()
	if _, err := p.Invoke(context.Background(), req); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := stub.lastReq.Messages[0]
	if len(got.Images) != 1 || got.Content != "<image_loaded>\n/tmp/a.png\n</image_loaded>" {
		t.Fatalf("vision model should keep images: %+v", got)
	}
	_ = png1x1
}

func TestNoImagesPassthrough(t *testing.T) {
	stub := &stubProvider{}
	p := Warp(func() bool { return false })(nil, stub)
	req := &types.ModelRequest{Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}}}
	before := req.Messages
	if _, err := p.Invoke(context.Background(), req); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if &req.Messages[0] != &before[0] {
		t.Fatalf("image-free request should not be rebuilt")
	}
}
