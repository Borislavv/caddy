package advancedcache

import (
	"bytes"
	"github.com/caddyserver/caddy/v2/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/zeebo/xxh3"
	"runtime"
	"strconv"
	"time"
)

// runControllerLogger runs a goroutine to periodically log RPS and avg duration per window, if debug enabled.
func (middleware *CacheMiddleware) runControllerLogger() {
	if !middleware.cfg.Cache.Logs.Stats {
		return
	}

	go func() {
		counter := 0
		const maxErrorsMapLength = 1024
		uniqueErrors := make(map[uint64]string, maxErrorsMapLength)
		ticker := utils.NewTicker(middleware.ctx, time.Second*5)
		for {
			select {
			case <-middleware.ctx.Done():
				return
			case <-middleware.counterCh:
				counter++
				runtime.Gosched()
			case err := <-middleware.errorCh:
				if len(uniqueErrors) < maxErrorsMapLength {
					errBytes := removeBraces([]byte(err.Error()))
					uniqueErrors[xxh3.Hash(errBytes)] = string(errBytes)
				}
			case <-ticker:
				middleware.writeLog(counter, uniqueErrors)
				counter = 0
				runtime.Gosched()
			}
		}
	}()
}

func removeBraces(s []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(s))
	start := 0

	for {
		open := bytes.IndexByte(s[start:], '{')
		if open == -1 {
			buf.Write(s[start:])
			break
		}
		open += start

		cls := bytes.IndexByte([]byte(s[open:]), '}')
		if cls == -1 {
			// unmatched '{', copy rest
			buf.Write(s[start:])
			break
		}
		cls += open

		// Write up to '{'
		buf.Write(s[start:open])
		// Skip over '{...}'
		start = cls + 1
	}

	return buf.Bytes()
}

// writeLog prints and resets stat counters for a given window (5s).
func (middleware *CacheMiddleware) writeLog(cnt int, errors map[uint64]string) {
	const secs int = 5

	if cnt <= 0 {
		return
	}

	var (
		avg string
		dur = time.Duration(secs / cnt)
		rps = strconv.Itoa(cnt / secs)
	)

	avg = (dur / time.Duration(cnt)).String()

	logEvent := log.Info()

	if middleware.cfg.IsProd() {
		logEvent.
			Str("target", "controller").
			Str("rps", rps).
			Str("served", strconv.Itoa(cnt)).
			Str("periodMs", "5000").
			Str("avgDuration", avg)
	}

	logEvent.Msgf("[controller][5s] served %d requests (rps: %s, avgDuration: %s)", cnt, rps, avg)

	for h, err := range errors {
		log.Error().Msg("[upstream][5s] error: " + err)
		delete(errors, h)
	}
}
