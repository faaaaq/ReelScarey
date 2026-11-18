package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/tidal"
)

// tidalLogin runs TIDAL's device authorization flow from the command line.
// Device login is inherently interactive: the owner must visit the link and
// enter the code in a browser, so this is an explicit local-owner command,
// not a UI action and never an automatic background step. The resulting
// token lives only in the tidal config directory and is never printed.
func tidalLogin(ctx context.Context, cfg config.Config) error {
	provider := os.Getenv("REELSCAREY_TIDAL_PROVIDER")
	if provider == "" {
		return errors.New("set REELSCAREY_TIDAL_PROVIDER to the tidal provider script before logging in")
	}
	python := os.Getenv("REELSCAREY_TIDAL_PYTHON")
	if python == "" {
		python = "python3"
	}
	configDir := os.Getenv("REELSCAREY_TIDAL_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Join(cfg.ConfigDir, "tidal")
	}
	service, err := tidal.New(python, provider, configDir, os.Getenv("REELSCAREY_TIDAL_QUALITY"))
	if err != nil {
		return err
	}
	if err := service.LoginBegin(ctx); err != nil {
		return fmt.Errorf("starting the TIDAL login failed: %w", err)
	}
	fmt.Println("Starting TIDAL login. Follow the printed link and code in a browser; this command finishes when TIDAL accepts it.")
	seen := 0
	deadline := time.After(9 * time.Minute)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("the TIDAL login timed out before TIDAL accepted the device code")
		case <-deadline:
			return errors.New("the TIDAL login timed out before TIDAL accepted the device code")
		case <-tick.C:
			state, err := service.LoginStatus(ctx)
			if err != nil {
				continue
			}
			for seen < len(state.Lines) {
				fmt.Println(state.Lines[seen])
				seen++
			}
			switch state.Phase {
			case "done":
				if state.Result == "success" {
					fmt.Println("TIDAL login succeeded. The token is stored in the tidal config directory and is backed up with the configuration.")
					return nil
				}
				message := state.Error
				if message == "" {
					message = "TIDAL did not accept the login."
				}
				return errors.New(message)
			case "idle":
				return errors.New("the provider reported no active login; run tidal-login again")
			}
		}
	}
}
