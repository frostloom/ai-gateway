// Package web 面板静态资源。由独立前端工程（web/）构建，产物在 dist/ 下：
//
//	cd web && npm install && npm run build
//
// dist/portal.html  → /portal 用户商城（Bearer 租户 key）
// dist/dashboard.html → /       管理面板（admin 登录态）
// dist/assets/*      → JS/CSS/字体（Vite 哈希产物）
//
// 由 gateway 直接托管，与 admin 接口同源；构建产物缺失时 go:embed 会编译失败
// （提示先构建前端），运行期 Dist() 只做一次 fs.Sub。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回 dist/ 子树（portal.html / dashboard.html / assets/*）。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web dist 构建产物缺失：请在 web/ 目录执行 npm install && npm run build 后重新 go build")
	}
	return sub
}