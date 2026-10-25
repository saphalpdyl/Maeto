package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/saphalpdyl/maeto/libs/store"
)

const SchemaVersion int32 = 1

type BootMode string

const (
	BootCold  BootMode = "cold"
	BootWarm  BootMode = "warm"
	BootReset BootMode = "reset"
)

func topologyFingerprint(statePath string, dataDir string) (string, error) {
	basePath, err := readLatestTopologyBasePath(statePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve build directory: %w", err)
	}

	topologyPath := filepath.Join(dataDir, basePath, TOPOLOGY_DATA_FILENAME)
	topologyData, err := os.ReadFile(topologyPath) // #nosec
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", topologyPath, err)
	}

	tenantPath := filepath.Join(dataDir, basePath, TENANT_DB_FILENAME)
	tenantData, err := os.ReadFile(tenantPath) // #nosec
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", tenantPath, err)
	}

	hash := sha256.New()
	hash.Write(topologyData)
	hash.Write(tenantData)

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func bootStore(
	ctx context.Context,
	db *store.DataStore,
	config Config,
	tenants *JSONTenantRepository,
	logger *slog.Logger,
) (BootMode, error) {
	fingerprint, err := topologyFingerprint(config.StatePath, config.DataDir)
	if err != nil {
		return "", err
	}

	state, err := db.Queries.GetControlState(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get control state: %w", err)
	}

	if state.SchemaVersion != SchemaVersion {
		return "", fmt.Errorf(
			"database schema is v%d but the control plane expects v%d, run make db-reset",
			state.SchemaVersion,
			SchemaVersion,
		)
	}

	mode := BootWarm
	if state.Fingerprint == "" {
		mode = BootCold
	} else if state.Fingerprint != fingerprint {
		mode = BootReset
	}

	if mode == BootWarm {
		if err := tenants.LoadFrom(ctx, db.Queries); err != nil {
			return "", fmt.Errorf("failed to load tenants from database: %w", err)
		}
	} else {
		if err := tenants.Load(ctx); err != nil {
			return "", fmt.Errorf("failed to load tenants from file: %w", err)
		}

		err := db.InTx(ctx, func(q *store.Queries) error {
			if err := q.DeleteAllSIDAllocations(ctx); err != nil {
				return fmt.Errorf("failed to delete sid allocations: %w", err)
			}

			if err := q.SetSIDCursor(ctx, 0); err != nil {
				return fmt.Errorf("failed to reset sid cursor: %w", err)
			}

			if err := tenants.SaveTo(ctx, q); err != nil {
				return err
			}

			if err := q.SetFingerprint(ctx, fingerprint); err != nil {
				return fmt.Errorf("failed to set fingerprint: %w", err)
			}

			return nil
		})
		if err != nil {
			return "", err
		}
	}

	err = db.Queries.SetBootedAt(ctx, pgtype.Timestamptz{Time: time.Now(), Valid: true})
	if err != nil {
		return "", fmt.Errorf("failed to set boot time: %w", err)
	}

	logger.InfoContext(ctx, "data store ready",
		slog.String("boot_mode", string(mode)),
		slog.String("fingerprint", fingerprint),
	)

	return mode, nil
}
