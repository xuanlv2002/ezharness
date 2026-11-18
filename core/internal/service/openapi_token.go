/*
OpenAPI 令牌：/api/open/* 面的访问凭证，多枚、可命名、可吊销，由用户在
安全页生成；持久于数据目录 openapi.json（0600）。与登录会话（auth.json）
分开——会话给人驾的端，令牌给 agent 产物（脚本、快应用、外部程序）。
最新一枚写进子进程环境 EZHARNESS_OPENAPI_TOKEN（终端里的脚本随手可用，
正式做法是读 openapi.json 拿实时清单）。
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
	"os"
	"sync"
	"time"

	"ezharness/core/internal/osfs"
)

/* OpenapiTokenEnv 终端与快应用后端等子进程继承最新 openapi 令牌的环境变量名。 */
const OpenapiTokenEnv = "EZHARNESS_OPENAPI_TOKEN"

/* openapiTokenFile 令牌落盘文件（数据目录内，相对路径）。 */
const openapiTokenFile = "openapi.json"

/* OpenapiToken 是一枚命名令牌。 */
type OpenapiToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

/* OpenapiStore 持有全部 openapi 令牌，中间件实时读取。 */
type OpenapiStore struct {
	mu     sync.Mutex
	tokens []OpenapiToken
}

/* LoadOpenapiStore 读取令牌清单；缺失/损坏/旧单令牌格式一律从空清单开始。 */
func LoadOpenapiStore() *OpenapiStore {
	o := &OpenapiStore{}
	fsys := osfs.OS{}
	if data, err := fsys.Read(context.Background(), openapiTokenFile); err == nil {
		var doc struct {
			Tokens []OpenapiToken `json:"tokens"`
		}
		if json.Unmarshal(data, &doc) == nil {
			o.tokens = doc.Tokens
		}
	}
	o.applyEnv()
	return o
}

/* Create 生成新令牌并落盘。 */
func (o *OpenapiStore) Create(name string) OpenapiToken {
	if name == "" {
		name = "未命名"
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	tok := make([]byte, 18)
	id := make([]byte, 4)
	if _, err := rand.Read(tok); err != nil {
		log.Fatal(err)
	}
	if _, err := rand.Read(id); err != nil {
		log.Fatal(err)
	}
	t := OpenapiToken{
		ID:        hex.EncodeToString(id),
		Name:      name,
		Token:     hex.EncodeToString(tok),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	o.tokens = append(o.tokens, t)
	o.save()
	o.applyEnv()
	return t
}

/* Revoke 按 id 吊销令牌。 */
func (o *OpenapiStore) Revoke(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, t := range o.tokens {
		if t.ID == id {
			o.tokens = append(o.tokens[:i], o.tokens[i+1:]...)
			o.save()
			o.applyEnv()
			return nil
		}
	}
	return fmt.Errorf("令牌不存在: %s", id)
}

/* List 令牌清单（安全页展示，token 值可见便于复制进快应用）。 */
func (o *OpenapiStore) List() []OpenapiToken {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]OpenapiToken, len(o.tokens))
	copy(out, o.tokens)
	return out
}

/* Verify 恒定时间校验令牌，命中返回该令牌（审计要记 id/name）。 */
func (o *OpenapiStore) Verify(token string) (OpenapiToken, bool) {
	if token == "" {
		return OpenapiToken{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, t := range o.tokens {
		if subtle.ConstantTimeCompare([]byte(t.Token), []byte(token)) == 1 {
			return t, true
		}
	}
	return OpenapiToken{}, false
}

func (o *OpenapiStore) save() {
	doc := struct {
		Tokens []OpenapiToken `json:"tokens"`
	}{Tokens: o.tokens}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	fsys := osfs.OS{}
	if err := fsys.WritePrivate(context.Background(), openapiTokenFile, data); err != nil {
		log.Printf("openapi 令牌落盘失败: %v", err)
	}
}

/*
	applyEnv 把最新一枚令牌写进进程环境（子进程继承；无令牌则清掉）。

调用方持锁或初始化路径。
*/
func (o *OpenapiStore) applyEnv() {
	if len(o.tokens) == 0 {
		_ = os.Unsetenv(OpenapiTokenEnv)
		return
	}
	_ = os.Setenv(OpenapiTokenEnv, o.tokens[len(o.tokens)-1].Token)
}
