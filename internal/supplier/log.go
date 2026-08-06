package supplier

import (
	"fmt"
	"os"
	"strings"
)

// debugEnabled turns on verbose request/response body logging. Bodies can
// contain prompts and signed URLs, so they stay off unless explicitly asked for.
var debugEnabled = func() bool {
	v := strings.TrimSpace(os.Getenv("MEDIA_MCP_DEBUG"))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}()

func logf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "[media-mcp] "+format+"\n", a...)
}

func debugf(format string, a ...interface{}) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "[media-mcp][debug] "+format+"\n", a...)
	}
}
