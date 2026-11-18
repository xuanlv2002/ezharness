/*
OpenAPI 审计：/api/open/* 每笔调用一行 jsonl，落数据目录 audit/openapi.jsonl，
超过 5MB 轮转为 .1（只留一代）。安全页倒序看最近 N 条。
*/
package service

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/* OpenapiAuditEntry 是一笔 openapi 调用记录。 */
type OpenapiAuditEntry struct {
	Time      string `json:"time"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	TokenID   string `json:"token_id"`
	TokenName string `json:"token_name"`
	Status    int    `json:"status"`
}

const (
	auditOpenapiFile  = "audit/openapi.jsonl"
	auditRotateBytes  = 5 << 20
	auditDefaultLimit = 100
)

var auditMu sync.Mutex

/* AppendOpenapiAudit 追加一笔审计（写入失败只记日志，不影响请求）。 */
func AppendOpenapiAudit(e OpenapiAuditEntry) {
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(auditOpenapiFile), 0o755); err != nil {
		return
	}
	if info, err := os.Stat(auditOpenapiFile); err == nil && info.Size() > auditRotateBytes {
		_ = os.Rename(auditOpenapiFile, auditOpenapiFile+".1")
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(auditOpenapiFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

/* ReadOpenapiAuditTail 倒序返回最近 limit 条（0 用默认 100）。 */
func ReadOpenapiAuditTail(limit int) []OpenapiAuditEntry {
	if limit <= 0 {
		limit = auditDefaultLimit
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	// 空清单也回 []（不是 null）：前端直接读 entries.length
	entries := []OpenapiAuditEntry{}
	data, err := os.ReadFile(auditOpenapiFile)
	if err != nil {
		return entries
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e OpenapiAuditEntry
		if json.Unmarshal(line, &e) == nil {
			entries = append(entries, e)
		}
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	// 倒序：最近在前
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}
