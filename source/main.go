package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const appVersion = "1.1.0"

//go:embed all:web/dist
var webFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatalf("前端资源加载失败: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "" || p == "index.html" {
			serveIndex(w, r, sub)
			return
		}
		if st, err := fs.Stat(sub, p); err != nil || st.IsDir() {
			serveIndex(w, r, sub)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	b, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "前端资源缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func main() {
	addr := flag.String("addr", ":8080", "监听地址，例如 :8080 或 127.0.0.1:8080")
	dataDir := flag.String("data", "data", "数据目录（配置与邮件缓存）")
	demo := flag.Bool("demo", false, "使用演示数据启动（不连接真实邮箱）")
	setKey := flag.String("key", "", "临时设置访问密钥后退出")
	flag.Parse()

	abs, _ := filepath.Abs(*dataDir)
	cm, err := newConfigManager(abs)
	if err != nil {
		log.Fatalf("初始化配置失败: %v", err)
	}
	if *setKey != "" {
		if err := cm.SetLoginKey(*setKey); err != nil {
			log.Fatalf("设置密钥失败: %v", err)
		}
		fmt.Println("访问密钥已更新，请重新启动程序。")
		return
	}
	st, err := newStore(filepath.Join(abs, "emails"))
	if err != nil {
		log.Fatalf("初始化邮件缓存失败: %v", err)
	}
	if *demo {
		seedDemo(cm, st)
	}

	ai := newAIService(cm, st)
	ai.Start()
	fw := newForwarder(cm, st)
	fw.Start()
	srv := newServer(cm, st, ai, fw, staticHandler())
	if !*demo {
		go srv.startBackgroundSync()
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	cfg := cm.Get()
	fmt.Println("")
	fmt.Println("  ┌─────────────────────────────────────────────┐")
	fmt.Println("  │            MailDesk AI  ·  邮件管理台        │")
	fmt.Println("  └─────────────────────────────────────────────┘")
	fmt.Printf("  访问地址 : http://localhost%s\n", portOf(*addr))
	fmt.Printf("  数据目录 : %s\n", abs)
	if cfg.KeyIsDefault {
		fmt.Println("  访问密钥 : admin123  （首次登录后请在「设置 - 安全」中修改）")
	} else {
		fmt.Println("  访问密钥 : 已自定义")
	}
	if *demo {
		fmt.Println("  运行模式 : 演示数据（不会连接真实邮箱）")
	}
	fmt.Println("  安全策略 : 同一 IP 连续 3 次密钥错误将锁定，重启程序后解锁")
	fmt.Println("")

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("\n正在退出，写入缓存…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	st.Flush()
	fmt.Println("已退出。重启后 IP 锁定状态将解除。")
}

func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ":8080"
}
