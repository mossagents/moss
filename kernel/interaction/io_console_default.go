package interaction

import (
	"io"
	"os"
)

func defaultStdout() io.Writer { return os.Stdout }
func defaultStdin() io.Reader  { return os.Stdin }
