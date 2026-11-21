package log

import (
	"log/slog"
)

func Err(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}
	return slog.String("error", err.Error())
}
