// reset-admin 重置/修改管理员账号密码（忘记登录密码或想换账号时用）。
//
//	用法：MYSQL_DSN="root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local" \
//	        go run ./scripts/reset-admin -from admin -username zlx -password 新密码
//	缺省：-from admin；密码必须 ≥ 6 位。同时清空旧会话（避免残留登录态）。
//	成功后用新账号密码登录 /。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
)

func main() {
	from := flag.String("from", "admin", "要修改的现有用户名")
	username := flag.String("username", "admin", "目标用户名（改名）")
	password := flag.String("password", "", "新密码（不少于 6 位）")
	flag.Parse()
	if len(*password) < 6 {
		log.Fatalf("密码至少 6 位")
	}
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Fatalf("连接 MySQL: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("bcrypt: %v", err)
	}
	res := db.Model(&store.AdminUser{}).
		Where("username = ?", *from).
		Updates(map[string]any{"username": *username, "password_hash": string(hash), "status": 0})
	if res.Error != nil {
		log.Fatalf("更新失败: %v（可能目标用户名已存在）", res.Error)
	}
	if res.RowsAffected == 0 {
		log.Printf("用户 %q 不存在（首次使用请走 /admin/auth/initialized 初始化）", *from)
		os.Exit(1)
	}
	// 清空旧会话：改名后旧 cookie 对应的 username 会话应失效
	if err := db.Where("1 = 1").Delete(&store.AdminSession{}).Error; err != nil {
		log.Printf("清会话失败（不影响登录）: %v", err)
	}
	fmt.Printf("已改管理员账号：%q / 密码 %d 位 ✓（登录 http://localhost:18080/ ）\n", *username, len(*password))
}