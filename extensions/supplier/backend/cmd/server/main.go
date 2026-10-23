package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/server"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	migrate := flag.Bool("migrate", false, "apply supplier-only migrations and exit (use the module migration role)")
	flag.Parse()
	dsn := os.Getenv("SUPPLIER_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.Path == "" || u.Path == "/" || u.Path == "/sub2api" || u.Path == "/postgres" {
		return fmt.Errorf("SUPPLIER_DATABASE_URL must explicitly select a supplier-only database, never sub2api or postgres")
	}
	instance := os.Getenv("SUPPLIER_UPSTREAM_INSTANCE_ID")
	source := os.Getenv("SUPPLIER_UPSTREAM_SOURCE_SHA")
	if instance == "" || len(source) != 40 {
		return fmt.Errorf("explicit upstream instance UUID and 40-character source SHA required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("supplier database configuration invalid")
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return fmt.Errorf("supplier database connection unavailable")
	}
	store := repository.New(db, instance, source)
	if *migrate {
		if err = store.Migrate(ctx); err != nil {
			return fmt.Errorf("supplier migration failed; inspect isolated migration tests and database permissions")
		}
		log.Print("supplier migrations applied; server not started")
		return nil
	}
	if err = store.CheckMigrations(ctx); err != nil {
		return fmt.Errorf("supplier schema missing or incompatible: run explicit -migrate using the migration role")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SUPPLIER_SESSION_KEY_BASE64"))
	if err != nil {
		return fmt.Errorf("invalid session encryption key encoding")
	}
	vault, err := auth.NewVault(key)
	if err != nil {
		return err
	}
	keyID := os.Getenv("SUPPLIER_SESSION_KEY_ID")
	if keyID == "" {
		return fmt.Errorf("SUPPLIER_SESSION_KEY_ID required")
	}
	upstream, err := sub2api.New(os.Getenv("SUPPLIER_UPSTREAM_URL"), nil)
	if err != nil {
		return fmt.Errorf("upstream URL invalid")
	}
	var prefixes []netip.Prefix
	for _, raw := range strings.Split(os.Getenv("SUPPLIER_TRUSTED_PROXIES"), ",") {
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || p.Bits() == 0 {
			return fmt.Errorf("invalid or unrestricted trusted proxy prefix")
		}
		prefixes = append(prefixes, p)
	}
	svc := &auth.Service{Store: store, Upstream: upstream, Vault: vault, InstanceID: instance, KeyID: keyID, Local: store}
	adminService := &auth.Service{Store: store, Upstream: upstream, Vault: vault, InstanceID: instance, KeyID: keyID, Administrator: true}
	supplierService := &suppliers.Service{Store: store, Vault: vault}
	accountService := &accounts.Service{Store: store, Vault: vault, KeyID: keyID, IdentityKeyID: os.Getenv("SUPPLIER_IDENTITY_KEY_ID"), ProxyCapacity: 1}
	usageService := &usage.Service{Store: store}
	if machineKey := os.Getenv("SUPPLIER_ADMIN_API_KEY"); machineKey != "" {
		accountService.Upstream, err = sub2api.NewAccountClient(os.Getenv("SUPPLIER_UPSTREAM_URL"), machineKey)
		if err != nil {
			return fmt.Errorf("supplier account adapter configuration invalid")
		}
		usageService.Source = accountService.Upstream.(ports.UsageSource)
		accountService.IdentityKey, err = base64.StdEncoding.DecodeString(os.Getenv("SUPPLIER_IDENTITY_KEY_BASE64"))
		if err != nil || len(accountService.IdentityKey) != 32 || accountService.IdentityKeyID == "" {
			return fmt.Errorf("independent stable identity fingerprint key and key ID required")
		}
		accountService.QuarantineID, err = strconv.ParseInt(os.Getenv("SUPPLIER_QUARANTINE_GROUP_ID"), 10, 64)
		if err != nil || accountService.QuarantineID <= 0 {
			return fmt.Errorf("explicit quarantine group ID required")
		}
		for _, raw := range strings.Split(os.Getenv("SUPPLIER_APPROVED_GROUP_IDS"), ",") {
			if raw == "" {
				continue
			}
			id, e := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if e != nil || id <= 0 || id == accountService.QuarantineID {
				return fmt.Errorf("invalid approved group ID")
			}
			accountService.ApprovedGroups = append(accountService.ApprovedGroups, id)
		}
	}
	handler, err := server.New(svc, server.Config{Origin: os.Getenv("SUPPLIER_PUBLIC_ORIGIN"), InsecureLocalhost: os.Getenv("SUPPLIER_INSECURE_LOCALHOST") == "true", TrustedProxies: prefixes, StaticDir: os.Getenv("SUPPLIER_STATIC_DIR"), Accounts: accountService, Usage: usageService, Suppliers: supplierService, Admin: adminService})
	if err != nil {
		return err
	}
	addr := os.Getenv("SUPPLIER_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8091"
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopped)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Print("supplier service listening; R2 local identity/admin management active; manual Claude onboarding active; owned proxies required; new accounts remain paused")
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("supplier HTTP server failed")
	}
	return nil
}
