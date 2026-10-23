package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/maintenance"
	"github.com/faaaaq/ReelScarey/internal/operations"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const usage = "usage: reelscarey [version|healthcheck|setup-token|reset-password|doctor|backup NEW_FOLDER|backup-check BACKUP_FOLDER|restore BACKUP_FOLDER NEW_CONFIG_FOLDER]"

func command(ctx context.Context, cfg config.Config, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch {
	case len(args) == 1 && args[0] == "healthcheck":
		return healthcheck(cfg.ListenAddr)
	case len(args) == 2 && args[0] == "backup":
		m, e := maintenance.Backup(ctx, cfg, args[1], version)
		if e != nil {
			return e
		}
		fmt.Printf("Configuration backup created and verified: %d files, schema %d. Protect this folder; it includes the encryption key.\n", len(m.Files), m.Schema)
		return nil
	case len(args) == 2 && args[0] == "backup-check":
		m, e := maintenance.Verify(ctx, args[1])
		if e != nil {
			return e
		}
		fmt.Printf("Backup integrity and saved credentials verified: %d files, schema %d.\n", len(m.Files), m.Schema)
		return nil
	case len(args) == 3 && args[0] == "restore":
		_, e := maintenance.Restore(ctx, cfg, args[1], args[2])
		if e != nil {
			return e
		}
		fmt.Println("Configuration restored into the new folder. Sessions were revoked, schedules are paused, and workers await review in System checks.")
		return nil
	case len(args) == 1 && (args[0] == "setup-token" || args[0] == "reset-password" || args[0] == "doctor"):
		filename := filepath.Join(cfg.ConfigDir, "reelscarey.db")
		info, e := os.Lstat(filename)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("start this installation once before using administration commands")
		}
		guard, e := instance.Guard(cfg.ConfigDir, true)
		if e != nil {
			return e
		}
		defer guard.Close()
		if e = instance.CheckStart(cfg.ConfigDir); e != nil {
			return e
		}
		db, e := database.OpenCurrent(ctx, filename)
		if e != nil {
			return e
		}
		defer db.Close()
		if _, e = database.Inspect(ctx, db); e != nil {
			return e
		}
		switch args[0] {
		case "doctor":
			report, e := operations.New(db, cfg).Run(ctx)
			if e != nil {
				return e
			}
			if e = json.NewEncoder(os.Stdout).Encode(report); e != nil {
				return e
			}
			for _, check := range report.Checks {
				if check.State == "attention" {
					return errors.New("some installation checks need attention; see the report")
				}
			}
			return nil
		case "setup-token":
			var n int
			if e = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				return errors.New("the administrator is already configured")
			}
			raw, e := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
			if e != nil {
				return e
			}
			_, e = fmt.Fprintln(os.Stdout, base64.RawURLEncoding.EncodeToString(raw))
			return e
		case "reset-password":
			var n int
			if e = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return errors.New("the administrator must be configured first")
			}
			body, e := io.ReadAll(io.LimitReader(os.Stdin, 259))
			if e != nil {
				return errors.New("read the new password from standard input")
			}
			if len(body) > 258 {
				return auth.ErrPassword
			}
			password := strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
			account, e := auth.New(ctx, db, cfg.ConfigDir)
			if e != nil {
				return e
			}
			if e = account.ResetPassword(ctx, password); e != nil {
				return e
			}
			fmt.Println("Password updated. All sessions have been revoked.")
			return nil
		}
	}
	return errors.New(usage)
}
