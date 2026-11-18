/*
认证状态：密码哈希与全部已签发会话持久于数据目录 auth.json（0600）。
会话令牌由 init/login 签发：多端并存、core 重启不换、可吊销；OpenAPI
令牌另存 openapi.json（openapi_token.go）。
*/
package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"ezharness/core/internal/osfs"
)

/* Session 是一枚已签发的会话令牌。token 值只在签发响应里完整出现一次。 */
type Session struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

type authDoc struct {
	PasswordHash string    `json:"passwordHash"`
	Sessions     []Session `json:"sessions"`
}

/* AuthStore 持有密码哈希与会话清单，中间件实时读取。 */
type AuthStore struct {
	mu       sync.Mutex
	hash     string
	sessions []Session
}

const authStateFile = "auth.json"

/*
LoadAuthState 读取认证状态（cwd = 数据目录）；缺失或损坏视为未初始化，
首个客户端重新走 init。
*/
func LoadAuthState() *AuthStore {
	a := &AuthStore{}
	fsys := osfs.OS{}
	if data, err := fsys.Read(context.Background(), authStateFile); err == nil {
		var doc authDoc
		if json.Unmarshal(data, &doc) == nil {
			a.hash, a.sessions = doc.PasswordHash, doc.Sessions
		}
	}
	return a
}

func (a *AuthStore) save() {
	doc := authDoc{PasswordHash: a.hash, Sessions: a.sessions}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	fsys := osfs.OS{}
	if err := fsys.WritePrivate(context.Background(), authStateFile, data); err != nil {
		log.Printf("认证状态落盘失败: %v", err)
	}
}

/* Initialized 密码是否已初始化。 */
func (a *AuthStore) Initialized() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hash != ""
}

/* Init 设置密码并签发首枚会话（仅未初始化时开放）。 */
func (a *AuthStore) Init(password, client, ip string) (Session, error) {
	if len(password) < 6 {
		return Session{}, fmt.Errorf("密码至少 6 位")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hash != "" {
		return Session{}, fmt.Errorf("密码已初始化，请直接登录")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Session{}, err
	}
	a.hash = string(hash)
	s := a.mint(client, ip)
	a.sessions = append(a.sessions, s)
	a.save()
	return s, nil
}

/* Login 校验密码并签发新会话。 */
func (a *AuthStore) Login(password, client, ip string) (Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hash == "" {
		return Session{}, fmt.Errorf("密码尚未初始化")
	}
	if bcrypt.CompareHashAndPassword([]byte(a.hash), []byte(password)) != nil {
		return Session{}, fmt.Errorf("密码错误")
	}
	s := a.mint(client, ip)
	a.sessions = append(a.sessions, s)
	a.save()
	return s, nil
}

/* VerifySession 恒定时间校验会话令牌。 */
func (a *AuthStore) VerifySession(token string) bool {
	if token == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sessions {
		if subtle.ConstantTimeCompare([]byte(s.Token), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

/* Logout 撤销令牌对应的会话；令牌本身即凭证，无需 id。 */
func (a *AuthStore) Logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.sessions[:0]
	for _, s := range a.sessions {
		if subtle.ConstantTimeCompare([]byte(s.Token), []byte(token)) != 1 {
			kept = append(kept, s)
		}
	}
	a.sessions = kept
	a.save()
}

/* SessionList 会话清单（不含 token 值），安全页展示。 */
func (a *AuthStore) SessionList() []Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Session, len(a.sessions))
	for i, s := range a.sessions {
		out[i] = Session{ID: s.ID, Name: s.Name, IP: s.IP, CreatedAt: s.CreatedAt}
	}
	return out
}

/* RevokeSession 按 id 撤销指定会话。 */
func (a *AuthStore) RevokeSession(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, s := range a.sessions {
		if s.ID == id {
			a.sessions = append(a.sessions[:i], a.sessions[i+1:]...)
			a.save()
			return nil
		}
	}
	return fmt.Errorf("会话不存在: %s", id)
}

/* mint 生成新会话（调用方持锁）。 */
func (a *AuthStore) mint(client, ip string) Session {
	if client == "" {
		client = "未命名"
	}
	tok := make([]byte, 18)
	id := make([]byte, 4)
	if _, err := rand.Read(tok); err != nil {
		log.Fatal(err)
	}
	if _, err := rand.Read(id); err != nil {
		log.Fatal(err)
	}
	return Session{
		ID:        hex.EncodeToString(id),
		Token:     hex.EncodeToString(tok),
		Name:      client,
		IP:        ip,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
