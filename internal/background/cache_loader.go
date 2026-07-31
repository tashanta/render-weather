// internal/background/cache_loader.go
package background

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
)

type CachePreloader interface {
	PreloadFromRedis(ctx context.Context) error
}

// StartCacheLoader starts background goroutine to preload cache from Redis
func StartCacheLoader(cache CachePreloader) {
	go func() {
		log.Info().Msg("starting cache preload from Redis")

		b := backoff.NewExponentialBackOff()
		b.InitialInterval = 1 * time.Second
		b.Multiplier = 2.0
		b.MaxInterval = 4 * time.Second
		b.MaxElapsedTime = 15 * time.Second

		operation := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := cache.PreloadFromRedis(ctx); err != nil {
				log.Warn().Err(err).Msg("cache preload attempt failed, will retry")
				return err
			}
			return nil
		}

		if err := backoff.Retry(operation, b); err != nil {
			log.Error().Err(err).Msg("cache preload failed after retries, starting with empty L1")
		} else {
			log.Info().Msg("cache preload completed successfully")
		}
	}()
}
