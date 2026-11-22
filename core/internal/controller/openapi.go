/*
OpenAPI 面：与 core 的会话认证完全分开，只认 EzHarnessOpenToken。登录态、
cookie、会话头都不参与；每笔调用（含 401）落审计。令牌管理本身在会话面，
是给用户安全页用的。
*/
package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

const openapiTokenHeader = "EzHarnessOpenToken"

/* OpenapiController 是 OpenAPI 令牌与审计端点（安全页数据面）。 */
type OpenapiController struct {
	Store *service.OpenapiStore
}

/* Tokens GET /api/security/openapi-tokens：令牌清单。 */
func (c *OpenapiController) Tokens(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"tokens": c.Store.List()})
}

/* Create POST /api/security/openapi-tokens：生成命名令牌。 */
func (c *OpenapiController) Create(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t := c.Store.Create(req.Name)
	g.JSON(http.StatusOK, gin.H{"token": t})
}

/* Revoke DELETE /api/security/openapi-tokens/:id：吊销令牌。 */
func (c *OpenapiController) Revoke(g *gin.Context) {
	if err := c.Store.Revoke(g.Param("id")); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.Status(http.StatusNoContent)
}

/* Audit GET /api/security/openapi-audit?limit=：最近审计记录（倒序）。 */
func (c *OpenapiController) Audit(g *gin.Context) {
	limit := 0
	if v := g.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	g.JSON(http.StatusOK, gin.H{"entries": service.ReadOpenapiAuditTail(limit)})
}

/*
openGuard 校验 OpenAPI 面：只认 EzHarnessOpenToken，不接受会话头/cookie。
失败只回 401（不触发任何登录流程），每笔调用都落审计。
*/
func openGuard(openapi *service.OpenapiStore) gin.HandlerFunc {
	return func(g *gin.Context) {
		if t, ok := openapi.Verify(g.GetHeader(openapiTokenHeader)); ok {
			g.Set(ctxTokenID, t.ID)
			g.Set(ctxTokenName, t.Name)
		} else {
			/* 先写响应再审计：审计读 Writer.Status()，顺序反了会把 401 记成 200 */
			g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			auditOpenapi(g)
			return
		}
		g.Next()
		auditOpenapi(g)
	}
}

/* ctxTokenID/Name 是 openGuard 写进 gin context 的审计字段名。 */
const (
	ctxTokenID   = "openapiTokenID"
	ctxTokenName = "openapiTokenName"
)

/* auditOpenapi 记一笔 openapi 调用审计（openGuard 专用）。 */
func auditOpenapi(g *gin.Context) {
	id, _ := g.Get(ctxTokenID)
	name, _ := g.Get(ctxTokenName)
	entryID, entryName := "", ""
	if s, ok := id.(string); ok {
		entryID = s
	}
	if s, ok := name.(string); ok {
		entryName = s
	}
	service.AppendOpenapiAudit(service.OpenapiAuditEntry{
		Method:    g.Request.Method,
		Path:      g.Request.URL.Path,
		TokenID:   entryID,
		TokenName: entryName,
		Status:    g.Writer.Status(),
	})
}
