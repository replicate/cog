package console

import (
	"time"

	"github.com/xeonx/timeago"
)

func FormatTime(t time.Time) string {
	return timeago.English.Format(t)
}
