package service

import (
	"os"
	"testing"
)

// 认证状态全链路：启动补默认密码、login 签发多端会话、logout/revoke 撤销、
// 落盘后重载仍有效（重启不换令牌）。
func TestAuthStateLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	a := LoadAuthState()
	a.EnsureDefaultPassword()
	if _, err := a.Login("wrong-pw", "端", "127.0.0.1"); err == nil {
		t.Fatal("wrong password should fail")
	}
	first, err := a.Login(DefaultPassword, "desktop", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !a.VerifySession(first.Token) {
		t.Fatal("first session should verify")
	}
	second, err := a.Login(DefaultPassword, "浏览器", "192.168.1.9")
	if err != nil {
		t.Fatal(err)
	}

	// 重载（模拟 core 重启）：默认密码已匹配就不动会话
	re := LoadAuthState()
	re.EnsureDefaultPassword()
	if !re.VerifySession(first.Token) || !re.VerifySession(second.Token) {
		t.Fatal("sessions should survive restart")
	}
	if _, err := re.Login(DefaultPassword, "x", "127.0.0.1"); err != nil {
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

/* 旧安装的自定义密码/损坏哈希：启动收敛回默认密码，旧会话一并清空。 */
func TestEnsureDefaultPasswordResets(t *testing.T) {
	t.Chdir(t.TempDir())
	old := []byte(`{"passwordHash":"broken","sessions":[{"id":"dead","token":"old-token"}]}`)
	if err := os.WriteFile(authStateFile, old, 0o600); err != nil {
		t.Fatal(err)
	}
	a := LoadAuthState()
	a.EnsureDefaultPassword()
	if a.VerifySession("old-token") {
		t.Fatal("old sessions should be cleared when password resets")
	}
	if _, err := a.Login(DefaultPassword, "cli", "127.0.0.1"); err != nil {
		t.Fatalf("default password should work after reset: %v", err)
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

/* 启动保证有默认令牌：空清单补一枚，已有令牌不重复补，env 跟着最新一枚。 */
func TestEnsureDefaultOpenapiToken(t *testing.T) {
	t.Chdir(t.TempDir())
	o := LoadOpenapiStore()
	o.EnsureDefaultToken()
	list := o.List()
	if len(list) != 1 || list[0].Name != "默认" {
		t.Fatalf("default token = %+v", list)
	}
	if got := os.Getenv(OpenapiTokenEnv); got != list[0].Token {
		t.Fatalf("env token = %q, want %q", got, list[0].Token)
	}
	o.EnsureDefaultToken()
	re := LoadOpenapiStore()
	re.EnsureDefaultToken()
	if len(re.List()) != 1 {
		t.Fatalf("existing token should not be duplicated: %+v", re.List())
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
