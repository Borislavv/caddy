package serverutils

import (
	"context"
	"errors"
	"github.com/caddyserver/caddy/v2/pkg/server/keyword"
	"github.com/valyala/fasthttp"
)

var CtxWasNotFoundError = errors.New("context.Context was not found into *fasthttp.RequestCtx")

func ExtractCtx(ctx *fasthttp.RequestCtx) (context.Context, error) {
	if reqCtx, ok := ctx.UserValue(keyword.CtxKey).(context.Context); ok {
		return reqCtx, nil
	}
	return nil, CtxWasNotFoundError
}
