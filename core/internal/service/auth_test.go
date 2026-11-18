package service

import (
	"os"
	"testing"
)

// 认证状态全链路：init 只许一次、login 签发多端会话、logout/revoke 撤销、
// 落盘后重载仍有效（重启不换令牌）。
func TestAuthStateLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	a := LoadAuthState()
	if a.Initialized() {
		t.Fatal("fresh state should be uninitialized")
	}
	if _, err := a.Login("pw123456", "端", "127.0.0.1"); err == nil {
		t.Fatal("login before init should fail")
	}
	if _, err := a.Init("123", "端", "127.0.0.1"); err == nil {
		t.Fatal("short password should fail")
	}
	first, err := a.Init("pw123456", "desktop", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Init("again123", "cli", "127.0.0.1"); err == nil {
		t.Fatal("second init should fail")
	}
	if !a.VerifySession(first.Token) {
		t.Fatal("first session should verify")
	}
	second, err := a.Login("pw123456", "浏览器", "192.168.1.9")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login("wrong-pw", "x", "127.0.0.1"); err == nil {
		t.Fatal("wrong password should fail")
	}

	// 重载（模拟 core 重启）：会话仍在、密码仍对
	re := LoadAuthState()
	if !re.VerifySession(first.Token) || !re.VerifySession(second.Token) {
		t.Fatal("sessions should survive restart")
	}
	if _, err := re.Login("pw123456", "x", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	// logout 只撤销自己；revoke 按 id 撤销
	re.Logout(first.Token)
	if re.VerifySession(first.Token) {
		t.Fatal("logged-out session should fail")
	}
	if !re.VerifySession(second.Token) {
		t.Fatal("other session should stay")
	}
	list := re.SessionList()
	if len(list) != 2 || list[0].Token != "" || list[0].ID == "" {
		t.Fatalf("session list should hide tokens and have ids: %+v", list)
	}
	if list[0].IP != "192.168.1.9" {
		t.Fatalf("session list should carry login ip: %+v", list[0])
	}
	if err := re.RevokeSession(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(authStateFile); err != nil {
		t.Fatalf("auth state file missing: %v", err)
	}
}

/* 审计文件缺失时回空清单而不是 nil（JSON 回 []，前端直接读 .length）。 */
func TestAuditEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	entries := ReadOpenapiAuditTail(0)
	if entries == nil || len(entries) != 0 {
		t.Fatalf("empty audit = %#v", entries)
	}
}

/* OpenAPI 多令牌：create/verify/revoke 与落盘重载。 */
func TestOpenapiStore(t *testing.T) {
	t.Chdir(t.TempDir())
	o := LoadOpenapiStore()
	if len(o.List()) != 0 {
		t.Fatal("fresh store should be empty")
	}
	a := o.Create("脚本")
	b := o.Create("快应用")
	if _, ok := o.Verify(a.Token); !ok {
		t.Fatal("token a should verify")
	}
	if got, _ := o.Verify(b.Token); got.Name != "快应用" {
		t.Fatalf("verify should return the token: %+v", got)
	}
	if err := o.Revoke(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Verify(a.Token); ok {
		t.Fatal("revoked token should fail")
	}
	re := LoadOpenapiStore()
	if _, ok := re.Verify(b.Token); !ok {
		t.Fatal("token should survive reload")
	}
	if got := os.Getenv(OpenapiTokenEnv); got != b.Token {
		t.Fatalf("env should carry newest token: %q", got)
	}
}
