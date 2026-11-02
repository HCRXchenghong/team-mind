package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/teammind/platform/internal/live"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "generate-vapid-key" {
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(base64.RawURLEncoding.EncodeToString(privateKey.D.FillBytes(make([]byte, 32))))
		return
	}
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	handler, err := live.NewServer()
	if err != nil {
		log.Fatal(err)
	}
	defer handler.DB.Close()
	if len(os.Args) > 1 && os.Args[1] == "wechat-worker" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := handler.RunWechatWorker(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "notification-worker" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		handler.RunNotificationWorker(ctx)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-invite" {
		code, expires, err := live.CreateSuperAdminInvite(handler.DB, 24*time.Hour)
		if err != nil {
			log.Fatal(err)
		}
		path := "/superadmin-setup?code=" + code
		fmt.Printf("SUPERADMIN_INVITE_CODE=%s\n", code)
		fmt.Printf("SUPERADMIN_INVITE_PATH=%s\n", path)
		if base := strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"); base != "" {
			fmt.Printf("SUPERADMIN_INVITE_URL=%s%s\n", base, path)
		}
		fmt.Printf("SUPERADMIN_INVITE_EXPIRES=%s\n", expires.Format(time.RFC3339))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-admin" {
		if err := live.BootstrapAdmin(handler.DB, os.Getenv("BOOTSTRAP_EMAIL"), os.Getenv("BOOTSTRAP_NAME"), os.Getenv("BOOTSTRAP_PASSWORD")); err != nil {
			log.Fatal(err)
		}
		log.Print("initial super admin created")
		return
	}
	server := &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		// SSE assistant replies and upstream model calls can legitimately take
		// longer than a normal request. Keep a bounded write deadline but leave
		// enough room for a full generated answer.
		WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go handler.RunReminderWorker(ctx)
	go handler.RunAIProactiveWorker(ctx)
	go handler.RunNotificationWorker(ctx)
	go handler.RunRetentionWorker(ctx)
	go handler.RunOutboxDispatcher(ctx)
	go handler.RunApprovalFollowupWorker(ctx)
	go handler.RunSandboxSessionCleaner(ctx)
	go handler.RunAgentMessageWorker(ctx)
	go handler.RunMemoryEmbeddingWorker(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("TeamMind API listening on %s (MySQL)", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
