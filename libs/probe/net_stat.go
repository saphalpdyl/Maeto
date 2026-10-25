package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func runNetStat(
	ctx context.Context,
	cfg *ProbeConfigNetStat,
	dispatcher Dispatcher,
	logger *slog.Logger,
) error {
	ticker := time.NewTicker(cfg.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		result := make(map[string]map[string]uint64)
		var resultMu sync.Mutex
		g, _ := errgroup.WithContext(ctx)

		for _, dev := range cfg.Devs {
			for _, stat := range cfg.StatItems {
				g.Go(func() error {
					fi, err := os.Open(fmt.Sprintf("/sys/class/net/%s/statistics/%s", dev, stat))
					if err != nil {
						return err
					}

					defer func() {
						if err := fi.Close(); err != nil {
							logger.WarnContext(ctx, "failed to close net stat file",
								slog.String("dev", dev),
								slog.String("stat", stat),
								slog.Any("error", err),
							)
						}
					}()

					buf := make([]byte, 32)
					n, err := fi.Read(buf)
					if err != nil {
						return err
					}

					v, err := strconv.ParseUint(strings.TrimSpace(string(buf[:n])), 10, 64)
					if err != nil {
						return err
					}

					resultMu.Lock()
					_, ok := result[dev]
					if !ok {
						result[dev] = make(map[string]uint64)
					}
					result[dev][stat] = v
					resultMu.Unlock()

					return nil
				})
			}
		}

		if err := g.Wait(); err != nil {
			logger.WarnContext(ctx, "failed to read net stats", slog.Any("error", err))
		}

		resultData, err := json.Marshal(NetStatResult{result})
		if err != nil {
			logger.WarnContext(ctx, "failed to marshal net stat results", slog.Any("error", err))
			continue
		}

		dispatch(ctx, dispatcher, Result{
			ProbeType:    ProbeTypeNetStat,
			TelemetryKey: "net_stat",
			SentAt:       time.Now(),
			Data:         resultData,
		}, logger)
	}
}
