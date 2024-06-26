package controldisplay

import (
	"context"
	"github.com/turbot/powerpipe/internal/controlexecute"
	"io"
)

type Formatter interface {
	Format(ctx context.Context, tree *controlexecute.ExecutionTree) (io.Reader, error)
	FileExtension() string
	Name() string
	Alias() string
}

type FormatterBase struct{}

func (*FormatterBase) Alias() string {
	return ""
}
