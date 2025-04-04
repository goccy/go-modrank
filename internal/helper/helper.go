package helper

import (
	"os"
	"path/filepath"
)

var TmpRoot = filepath.Join(os.TempDir(), "go-modrank")
