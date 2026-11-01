/*
OpenAPI 令牌：/api/open/* 面的访问凭证，持久于数据目录 openapi.json
（0600）。与前端会话令牌分开——那枚每次启动换、只活在进程内；这枚给
agent 产物用（脚本、快应用后端、硬编码进页面的场景），长期有效，
设置页手动重置换新。
*/
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"ezharness/core/internal/osfs"
)

/* OpenapiTokenEnv 终端与快应用后端等子进程继承 openapi 令牌的环境变量名。 */
const OpenapiTokenEnv = "EZHARNESS_OPENAPI_TOKEN"

/* openapiTokenFile 令牌落盘文件（数据目录内，相对路径）。 */
const openapiTokenFile = "openapi.json"

type openapiTokenDoc struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

var openapiMu sync.Mutex

/*
LoadOrCreateOpenapiToken 读取 openapi 令牌；缺失或损坏则生成新令牌落盘，
并把 Unix 权限收敛为 0600。启动与换代（切数据目录）时各调一次。
*/
func LoadOrCreateOpenapiToken() string {
	openapiMu.Lock()
	defer openapiMu.Unlock()
	fsys := osfs.OS{}
	ctx := context.Background()
	if data, err := fsys.Read(ctx, openapiTokenFile); err == nil {
		var doc openapiTokenDoc
		if json.Unmarshal(data, &doc) == nil && doc.Token != "" {
			fsys.Restrict(openapiTokenFile)
			return doc.Token
		}
		log.Printf("openapi 令牌文件不可用，重新生成（%s）", openapiTokenFile)
	}
	token := newOpenapiToken()
	if err := saveOpenapiToken(fsys, token); err != nil {
		log.Printf("openapi 令牌落盘失败（重启后将更换）: %v", err)
	}
	ApplyOpenapiTokenEnv(token)
	return token
}

/*
ResetOpenapiToken 换新令牌：落盘并更新子进程环境。旧令牌即时作废，
硬编码它的产物需要重新生成。
*/
func ResetOpenapiToken() string {
	openapiMu.Lock()
	defer openapiMu.Unlock()
	token := newOpenapiToken()
	if err := saveOpenapiToken(osfs.OS{}, token); err != nil {
		log.Printf("openapi 令牌落盘失败: %v", err)
	}
	ApplyOpenapiTokenEnv(token)
	return token
}

/* ApplyOpenapiTokenEnv 把令牌写进进程环境（子进程继承）。 */
func ApplyOpenapiTokenEnv(token string) { _ = os.Setenv(OpenapiTokenEnv, token) }

/* newOpenapiToken 生成 18 字节 hex 令牌。 */
func newOpenapiToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func saveOpenapiToken(fsys osfs.OS, token string) error {
	doc := openapiTokenDoc{Token: token, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return fsys.WritePrivate(context.Background(), openapiTokenFile, data)
}
